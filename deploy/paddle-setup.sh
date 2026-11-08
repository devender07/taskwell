#!/usr/bin/env bash
# Makes the Paddle objects billing needs, once per Paddle environment (sandbox, then live): the products "Taskwell
# Pro" and "Taskwell Ultimate" with one monthly price each (USD, with an INR override for India), and the notification
# destination at <api>/api/billing/webhook (docs/billing-plan.md). It prints the paddle_ config values. Products and
# prices found by custom_data.taskwell_plan, and a destination with the same URL, are reused. Needs curl and jq.
# Usage: deploy/paddle-setup.sh API_KEY https://api.taskwell.dev
#   PADDLE_API=https://api.paddle.com deploy/paddle-setup.sh ...   (live; the default is the sandbox)
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

# price PLAN NAME USD_CENTS INR_PAISE: echoes the plan's active monthly price id, making its product and price when
# missing.
price() {
  local product found
  product=$(paddle GET "/products?status=active&per_page=200" | jq -r --arg p "$1" '[.[] | select(.custom_data.taskwell_plan == $p)][0].id // empty')
  if [ -z "$product" ]; then
    product=$(paddle POST /products "$(jq -nc --arg n "$2" --arg p "$1" '{name: $n, tax_category: "saas", custom_data: {taskwell_plan: $p}}')" | jq -r .id)
  fi
  found=$(paddle GET "/prices?product_id=$product&status=active&per_page=200" | jq -r --arg p "$1" '[.[] | select(.custom_data.taskwell_plan == $p)][0].id // empty')
  if [ -n "$found" ]; then echo "$found"; return; fi
  paddle POST /prices "$(jq -nc --arg prod "$product" --arg n "$2" --arg p "$1" --arg usd "$3" --arg inr "$4" '{
    description: "\($n) monthly", name: "Monthly", product_id: $prod,
    unit_price: {amount: $usd, currency_code: "USD"},
    unit_price_overrides: [{country_codes: ["IN"], unit_price: {amount: $inr, currency_code: "INR"}}],
    billing_cycle: {interval: "month", frequency: 1}, custom_data: {taskwell_plan: $p}}')" | jq -r .id
}

PRO=$(price pro "Taskwell Pro" 2900 149900)
ULT=$(price ultimate "Taskwell Ultimate" 7900 399900)

SECRET=$(paddle GET /notification-settings | jq -r --arg url "$HOOK" '[.[] | select(.destination == $url)][0].endpoint_secret_key // empty')
if [ -z "$SECRET" ]; then
  SECRET=$(paddle POST /notification-settings "$(jq -nc --arg url "$HOOK" '{
    description: "Taskwell billing", type: "url", destination: $url, api_version: 1,
    subscribed_events: ["subscription.created", "subscription.updated", "subscription.activated",
      "subscription.past_due", "subscription.paused", "subscription.resumed", "subscription.canceled",
      "adjustment.created"]}')" | jq -r .endpoint_secret_key)
fi

echo "paddle_price_pro: $PRO"
echo "paddle_price_ultimate: $ULT"
echo "paddle_webhook_secret: $SECRET   (notification destination $HOOK)"
