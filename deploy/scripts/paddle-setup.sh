#!/usr/bin/env bash
# Makes billing's notification destination at <api>/api/billing/webhook, once per Paddle environment (sandbox, then
# live), and prints its secret for paddle_webhook_secret (docs/billing-plan.md). A destination with the same URL is
# reused. Products and prices are managed in the Paddle dashboard; every price needs custom_data.taskwell_plan.
# Needs curl and jq.
# Usage: deploy/scripts/paddle-setup.sh API_KEY https://api.taskwell.dev
#   PADDLE_API=https://api.paddle.com deploy/scripts/paddle-setup.sh ...   (live; the default is the sandbox)
set -euo pipefail
shopt -s inherit_errexit

[ $# -eq 2 ] || { echo "usage: $0 API_KEY API_BASE_URL"; exit 1; }
KEY=$1
HOOK=${2%/}/api/billing/webhook
API=${PADDLE_API:-https://sandbox-api.paddle.com}
command -v jq >/dev/null || { echo "jq is required"; exit 1; }

# paddle METHOD PATH [JSON body]: the reply's data, or Paddle's error on stderr. The key goes in on stdin, out of
# the process list.
paddle() {
  local method=$1 path=$2 out
  local args=(-sS --fail-with-body -K - -X "$method" "$API$path")
  [ $# -ge 3 ] && args+=(-H "Content-Type: application/json" --data-binary "$3")
  out=$(printf 'header = "Authorization: Bearer %s"\n' "$KEY" | curl "${args[@]}") ||
    { echo "paddle $method $path failed: $out" >&2; return 1; }
  jq -c .data <<<"$out"
}

SECRET=$(paddle GET /notification-settings | jq -r --arg url "$HOOK" '[.[] | select(.destination == $url)][0].endpoint_secret_key // empty')
if [ -z "$SECRET" ]; then
  SECRET=$(paddle POST /notification-settings "$(jq -nc --arg url "$HOOK" '{
    description: "Taskwell billing", type: "url", destination: $url, api_version: 1,
    subscribed_events: ["subscription.created", "subscription.updated", "subscription.activated",
      "subscription.past_due", "subscription.paused", "subscription.resumed", "subscription.canceled",
      "adjustment.created"]}')" | jq -r .endpoint_secret_key)
fi

echo "paddle_webhook_secret: $SECRET   (notification destination $HOOK)"
