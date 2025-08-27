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
  if [ ! -z "$APP_PID" ]; then
    kill "$APP_PID" 2>/dev/null || true
  fi
  sudo docker compose -f "$DOCKER_COMPOSE_FILE" down -v
  rm -f "$APP_LOG_FILE"
}
trap cleanup EXIT

start_app() {
  : > "$APP_LOG_FILE"
  export SQS_QUEUE_URL="$QUEUE_URL"
  export AWS_ENDPOINT_URL="$AWS_ENDPOINT_URL"
  /tmp/e2e-app > "$APP_LOG_FILE" 2>&1 &
  APP_PID=$!
  echo "Waiting for application to start..."
  sleep 5
}

send_message() {
  local TYPE="$1"
  local VERSION="$2"
  local PAYLOAD_JSON="$3"
  local MSG_ID="$(cat /proc/sys/kernel/random/uuid)"
  local TS="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  local BODY=$(cat <<EOF
{
  "schemaVersion": "1.0",
  "messageType": "$TYPE",
  "messageVersion": "$VERSION",
  "message": $PAYLOAD_JSON,
  "metadata": {
    "timestamp": "$TS",
    "source": "e2e-test-script",
    "messageId": "$MSG_ID"
  }
}
EOF
)
  aws --endpoint-url="$AWS_ENDPOINT_URL" sqs send-message --queue-url "$QUEUE_URL" --message-body "$BODY" >/dev/null
}

wait_for_log() {
  local PATTERN="$1"
  local TIMEOUT="${2:-30}"
  local COUNT=0
  local ITER=$(( TIMEOUT / 2 ))
  echo "--- Waiting for log: $PATTERN (timeout ${TIMEOUT}s) ---"
  for i in $(seq 1 $ITER); do
    if grep -qE "$PATTERN" "$APP_LOG_FILE"; then
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo
  echo "❌ Timeout waiting for: $PATTERN"
  echo "--- Application Logs ---"
  cat "$APP_LOG_FILE" || true
  exit 1
}

echo "--- Starting e2e test ---"

# 1) Start LocalStack
echo "--- Starting LocalStack container ---"
sudo docker compose -f "$DOCKER_COMPOSE_FILE" up -d

echo "--- Waiting for SQS service to be available ---"
until aws --endpoint-url="$AWS_ENDPOINT_URL" sqs list-queues &> /dev/null; do
  echo -n "."
  sleep 2
done
echo " SQS is ready."

echo "--- Creating SQS queue: $QUEUE_NAME ---"
QUEUE_URL=$(aws --endpoint-url="$AWS_ENDPOINT_URL" sqs create-queue --queue-name "$QUEUE_NAME" --query 'QueueUrl' --output text)
if [ -z "$QUEUE_URL" ]; then
  echo "❌ FAILED to create SQS queue."
  exit 1
fi
echo "Queue URL: $QUEUE_URL"

echo "--- Building the test application ---"
(
  cd "${TEST_DIR}/e2e"
  go build -o /tmp/e2e-app
)

echo "--- Scenario 1: Normal flow with middleware ---"
export E2E_MW_TOUCH=1
unset E2E_MW_FAIL
unset E2E_FAIL_FAST
start_app

TEST_ID1=$(cat /proc/sys/kernel/random/uuid)
send_message "e2eTest" "1.0" "{\"testId\": \"$TEST_ID1\", \"payload\": \"Hello from e2e test\"}"

wait_for_log "E2E_TEST_SUCCESS: Received message for test ID $TEST_ID1"
wait_for_log "E2E_MW_BEFORE type=e2eTest version=1.0"
wait_for_log "E2E_MW_AFTER_OK type=e2eTest version=1.0"
wait_for_log "MW_OVERRIDDEN"

echo "--- Scenario 2: No handler case still runs middleware ---"
send_message "unknownType" "1.0" "{\"foo\": \"bar\"}"
wait_for_log "E2E_MW_BEFORE type=unknownType version=1.0"
wait_for_log "E2E_MW_AFTER_ERR"

echo "--- Scenario 3: Schema validation error before handler ---"
TEST_ID2=$(cat /proc/sys/kernel/random/uuid)
send_message "e2eTest" "1.0" "{\"testId\": \"$TEST_ID2\"}"
wait_for_log "E2E_MW_BEFORE type=e2eTest version=1.0"
wait_for_log "E2E_MW_AFTER_ERR"
if grep -q "E2E_TEST_SUCCESS: Received message for test ID $TEST_ID2" "$APP_LOG_FILE"; then
  echo "❌ Unexpected success for invalid schema message"
  exit 1
fi

echo "--- Scenario 4: Fail-fast with middleware error ---"
kill "$APP_PID" 2>/dev/null || true
sleep 2
export E2E_FAIL_FAST=1
export E2E_MW_FAIL=1
unset E2E_MW_TOUCH
start_app

TEST_ID3=$(cat /proc/sys/kernel/random/uuid)
send_message "e2eTest" "1.0" "{\"testId\": \"$TEST_ID3\", \"payload\": \"should fail by mw\"}"
wait_for_log "E2E_FAIL_FAST_ENABLED"
wait_for_log "E2E_MW_BEFORE type=e2eTest version=1.0"
wait_for_log "E2E_MW_AFTER_ERR"

echo "✅ All E2E scenarios passed."
