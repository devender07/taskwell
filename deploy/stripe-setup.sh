#!/usr/bin/env bash
# Makes the Stripe objects billing needs, once per Stripe account (sandbox and live): the Pro and Ultimate products
# with their monthly prices (lookup keys pro_monthly and ultimate_monthly, USD with an INR option), the default
# customer portal configuration, and the webhook endpoint, whose signing secret it prints (docs/billing-plan.md).
# Prices found by lookup key and an endpoint with the same URL are reused. Needs curl and jq.
# Usage: deploy/stripe-setup.sh sk_test_... https://api.taskwell.dev   (a full secret key: it writes products and webhooks)
set -euo pipefail
shopt -s inherit_errexit

[ $# -eq 2 ] || { echo "usage: $0 SECRET_KEY API_BASE_URL"; exit 1; }
KEY=$1
HOOK=${2%/}/api/billing/webhook
VERSION=2026-09-30.endive
command -v jq >/dev/null || { echo "jq is required"; exit 1; }

# stripe METHOD PATH [curl args]: the reply, or Stripe's error on stderr. The key goes in on stdin, out of the
# process list.
stripe() {
  local method=$1 path=$2 out
  shift 2
  out=$(printf 'header = "Authorization: Bearer %s"\n' "$KEY" |
    curl -sS --fail-with-body -K - -H "Stripe-Version: $VERSION" -X "$method" "${STRIPE_API:-https://api.stripe.com}$path" "$@") ||
    { echo "stripe $method $path failed: $out" >&2; return 1; }
  printf '%s\n' "$out"
}

# price LOOKUP_KEY NAME USD_CENTS INR_PAISE: echoes the price id and its product id, making both when missing.
price() {
  local found product
  found=$(stripe GET /v1/prices -G -d active=true --data-urlencode "lookup_keys[]=$1" | jq -r '.data[0] // empty | "\(.id) \(.product)"')
  if [ -n "$found" ]; then echo "$found"; return; fi
  product=$(stripe POST /v1/products -d "name=$2" | jq -r .id)
  stripe POST /v1/prices -d "product=$product" -d "lookup_key=$1" -d currency=usd -d "unit_amount=$3" \
    -d "recurring[interval]=month" -d "currency_options[inr][unit_amount]=$4" | jq -r '"\(.id) \(.product)"'
}

P=$(price pro_monthly "Taskwell Pro" 2900 149900)
read -r PRO PRO_PRODUCT <<<"$P"
P=$(price ultimate_monthly "Taskwell Ultimate" 7900 399900)
read -r ULT ULT_PRODUCT <<<"$P"
echo "prices: pro_monthly $PRO, ultimate_monthly $ULT"

PORTAL=(
  -d "features[subscription_cancel][enabled]=true" -d "features[subscription_cancel][mode]=at_period_end"
  -d "features[payment_method_update][enabled]=true" -d "features[invoice_history][enabled]=true"
  -d "features[subscription_update][enabled]=true" -d "features[subscription_update][default_allowed_updates][]=price"
  -d "features[subscription_update][proration_behavior]=create_prorations"
  -d "features[subscription_update][products][0][product]=$PRO_PRODUCT" -d "features[subscription_update][products][0][prices][]=$PRO"
  -d "features[subscription_update][products][1][product]=$ULT_PRODUCT" -d "features[subscription_update][products][1][prices][]=$ULT"
)
CONF=$(stripe GET /v1/billing_portal/configurations -G -d is_default=true -d limit=1 | jq -r '.data[0].id // empty')
if [ -n "$CONF" ]; then
  stripe POST "/v1/billing_portal/configurations/$CONF" "${PORTAL[@]}" >/dev/null
else
  CONF=$(stripe POST /v1/billing_portal/configurations "${PORTAL[@]}" | jq -r .id)
fi
echo "portal configuration: $CONF"

ENDPOINT=$(stripe GET /v1/webhook_endpoints -G -d limit=100 | jq -r --arg url "$HOOK" '.data[] | select(.url == $url) | .id' | head -n1)
if [ -n "$ENDPOINT" ]; then
  echo "webhook endpoint $ENDPOINT already exists for $HOOK; its secret is in the Dashboard (Developers > Webhooks)"
  exit 0
fi
stripe POST /v1/webhook_endpoints -d "url=$HOOK" -d "api_version=$VERSION" \
  -d "enabled_events[]=checkout.session.completed" -d "enabled_events[]=customer.subscription.created" \
  -d "enabled_events[]=customer.subscription.updated" -d "enabled_events[]=customer.subscription.deleted" \
  -d "enabled_events[]=invoice.paid" -d "enabled_events[]=invoice.payment_failed" \
  -d "enabled_events[]=charge.dispute.created" -d "enabled_events[]=charge.refunded" |
  jq -r '"webhook endpoint \(.id) for \(.url)\nstripe_webhook_secret: \(.secret)"'
