#!/bin/bash
set -eo pipefail

# --- Configuration ---
TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${TEST_DIR}/.." && pwd)"
DOCKER_COMPOSE_FILE="${TEST_DIR}/docker-compose.yaml"
APP_LOG_FILE="/tmp/e2e-app.log"
QUEUE_NAME="e2e-test-queue"
AWS_ENDPOINT_URL="http://localhost:4566"
# --- AWS credentials defaults for LocalStack ---
# LocalStack requires AWS clients to have credentials/region set, but any dummy values work.
# These exports only apply when variables are not already set in the environment.

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"
export AWS_SESSION_TOKEN="${AWS_SESSION_TOKEN:-dummy}"
export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-ap-northeast-1}"


# --- Cleanup Function ---
cleanup() {
  echo "--- Cleaning up ---"
  if [ ! -z "$APP_PID" ]; then
    kill "$APP_PID" 2>/dev/null || true
  fi
  docker compose -f "$DOCKER_COMPOSE_FILE" down -v
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
  sleep 1
}

wait_for_log() {
  local PATTERN="$1"
  local TIMEOUT="${2:-60}"
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
docker compose -f "$DOCKER_COMPOSE_FILE" up -d

echo "--- Waiting for SQS service to be available ---"
SQS_WAIT_TIMEOUT="${SQS_WAIT_TIMEOUT:-60}"
SQS_WAIT_INTERVAL="${SQS_WAIT_INTERVAL:-2}"
SQS_WAIT_ELAPSED=0
SQS_READY=0
while [ "$SQS_WAIT_ELAPSED" -lt "$SQS_WAIT_TIMEOUT" ]; do
  if aws --endpoint-url="$AWS_ENDPOINT_URL" sqs list-queues &> /dev/null; then
    SQS_READY=1
    break
  fi
  echo -n "."
  sleep "$SQS_WAIT_INTERVAL"
  SQS_WAIT_ELAPSED=$(( SQS_WAIT_ELAPSED + SQS_WAIT_INTERVAL ))
done
if [ "$SQS_READY" -ne 1 ]; then
  echo
  echo "❌ Timeout waiting for SQS to become ready after ${SQS_WAIT_TIMEOUT}s."
  echo "--- docker logs (last 100 lines) ---"
  docker compose -f "$DOCKER_COMPOSE_FILE" logs --tail 100 || true
  exit 1
fi
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
wait_for_log "E2E_MW_BEFORE"
wait_for_log "E2E_MW_AFTER_OK type=e2eTest version=1.0"
wait_for_log "MW_OVERRIDDEN"

echo "--- Scenario 2: No handler case still runs middleware ---"
send_message "unknownType" "1.0" "{\"foo\": \"bar\"}"
wait_for_log "E2E_MW_BEFORE"
wait_for_log "E2E_MW_AFTER_ERR"

echo "--- Scenario 3: Schema validation error before handler ---"
TEST_ID2=$(cat /proc/sys/kernel/random/uuid)
send_message "e2eTest" "1.0" "{\"testId\": \"$TEST_ID2\"}"
wait_for_log "E2E_MW_BEFORE"
wait_for_log "E2E_MW_AFTER_ERR"
if grep -q "E2E_TEST_SUCCESS: Received message for test ID $TEST_ID2" "$APP_LOG_FILE"; then
  echo "❌ Unexpected success for invalid schema message"
  exit 1
fi

echo "--- Scenario 4: Middleware error does not force delete (policy default) ---"
kill "$APP_PID" 2>/dev/null || true
sleep 2
unset E2E_FAIL_FAST
export E2E_MW_FAIL=1
unset E2E_MW_TOUCH
start_app

TEST_ID3=$(cat /proc/sys/kernel/random/uuid)
send_message "e2eTest" "1.0" "{\"testId\": \"$TEST_ID3\", \"payload\": \"should fail by mw\"}"
wait_for_log "E2E_MW_BEFORE"
wait_for_log "E2E_MW_AFTER_ERR"
wait_for_log "RETRYING message ID"

echo "✅ All E2E scenarios passed."
