#!/bin/bash
set -eo pipefail

# --- Configuration ---
TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${TEST_DIR}/.." && pwd)"
DOCKER_COMPOSE_FILE="${TEST_DIR}/docker-compose.yaml"
APP_LOG_FILE="/tmp/e2e-app.log"
QUEUE_NAME="e2e-test-queue"
AWS_ENDPOINT_URL="http://localhost:4566"

# --- Cleanup Function ---
cleanup() {
  echo "--- Cleaning up ---"
  # Stop the app if it's still running
  if [ ! -z "$APP_PID" ]; then
    kill "$APP_PID" 2>/dev/null || true
  fi
  sudo docker compose -f "$DOCKER_COMPOSE_FILE" down -v
  rm -f "$APP_LOG_FILE"
}
trap cleanup EXIT

# --- Main Test Logic ---
echo "--- Starting e2e test ---"

# 1. Start LocalStack
echo "--- Starting LocalStack container ---"
sudo docker compose -f "$DOCKER_COMPOSE_FILE" up -d

# 2. Wait for SQS to be ready
echo "--- Waiting for SQS service to be available ---"
# The aws-cli needs to be installed in the environment.
# The sandbox should have it. If not, the script will fail here.
until aws --endpoint-url="$AWS_ENDPOINT_URL" sqs list-queues &> /dev/null; do
  echo -n "."
  sleep 2
done
echo " SQS is ready."

# 3. Create SQS Queue
echo "--- Creating SQS queue: $QUEUE_NAME ---"
QUEUE_URL=$(aws --endpoint-url="$AWS_ENDPOINT_URL" sqs create-queue --queue-name "$QUEUE_NAME" --query 'QueueUrl' --output text)
if [ -z "$QUEUE_URL" ]; then
  echo "❌ FAILED to create SQS queue."
  exit 1
fi
echo "Queue URL: $QUEUE_URL"

# 4. Build and Run the Go application
echo "--- Building and running the test application ---"
(
  cd "${TEST_DIR}/e2e"
  go build -o /tmp/e2e-app
)
export SQS_QUEUE_URL="$QUEUE_URL"
export AWS_ENDPOINT_URL="$AWS_ENDPOINT_URL"
/tmp/e2e-app > "$APP_LOG_FILE" 2>&1 &
APP_PID=$!

# Give the app a moment to start and connect
echo "Waiting for application to start..."
sleep 5

# 5. Send a test message
TEST_ID=$(cat /proc/sys/kernel/random/uuid)
echo "--- Sending test message with ID: $TEST_ID ---"
MESSAGE_BODY=$(cat <<EOF
{
  "schemaVersion": "1.0",
  "messageType": "e2eTest",
  "messageVersion": "1.0",
  "message": {
    "testId": "$TEST_ID",
    "payload": "Hello from e2e test"
  },
  "metadata": {
    "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
    "source": "e2e-test-script",
    "messageId": "$(cat /proc/sys/kernel/random/uuid)"
  }
}
EOF
)

aws --endpoint-url="$AWS_ENDPOINT_URL" sqs send-message --queue-url "$QUEUE_URL" --message-body "$MESSAGE_BODY"

# 6. Verify message processing
echo "--- Verifying message processing (will wait up to 30 seconds) ---"
for i in {1..15}; do
  if grep -q "E2E_TEST_SUCCESS: Received message for test ID $TEST_ID" "$APP_LOG_FILE"; then
    echo "✅ TEST PASSED: Found success message in log."
    exit 0
  fi
  echo -n "."
  sleep 2
done

echo "❌ TEST FAILED: Did not find success message in log after 30 seconds."
echo "--- Application Logs ---"
cat "$APP_LOG_FILE"
exit 1
