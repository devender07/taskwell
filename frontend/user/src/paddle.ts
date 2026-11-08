import { initializePaddle, type Paddle, type PricePreviewResponse } from '@paddle/paddle-js'

let paddle: Promise<Paddle> | undefined

export function getPaddle() {
  paddle ??= (async () => {
    const environment = import.meta.env.VITE_PADDLE_ENV
    const token = import.meta.env.VITE_PADDLE_CLIENT_TOKEN
    if (environment !== 'sandbox' && environment !== 'production') {
      throw new Error(`VITE_PADDLE_ENV must be "sandbox" or "production", got ${JSON.stringify(environment)}`)
    }
    if (!token) throw new Error('VITE_PADDLE_CLIENT_TOKEN is missing')
    const p = await initializePaddle({ environment, token })
    if (!p) throw new Error('Paddle.js failed to load')
    return p
  })()
  paddle.catch(() => (paddle = undefined))
  return paddle
}

let previews: Promise<Record<string, string>> | undefined

// Paddle's own formatted total for each price, localized to the visitor's country (Paddle reads it from the IP).
export function previewPrices(priceIds: string[]) {
  previews ??= getPaddle()
    .then((p) =>
      p.PricePreview({
        items: priceIds.map((priceId) => ({ priceId, quantity: 1 })),
      }),
    )
    .then((r: PricePreviewResponse) => Object.fromEntries(r.data.details.lineItems.map((l) => [l.price.id, l.formattedTotals.total])))
  previews.catch(() => (previews = undefined))
  return previews
}

export async function openCheckout(priceId: string, email: string, customData: Record<string, string>, successUrl: string) {
  const p = await getPaddle()
  p.Checkout.open({
    items: [{ priceId, quantity: 1 }],
    customer: { email },
    customData,
    settings: { displayMode: 'overlay', variant: 'one-page', successUrl },
  })
}
