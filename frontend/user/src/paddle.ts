import type { Checkout } from '@/api'

type Paddle = {
  Environment: { set(env: 'sandbox'): void }
  Initialize(opts: { token: string }): void
  Checkout: {
    open(opts: {
      transactionId: string
      customer: { email: string }
      settings: { displayMode: 'overlay'; successUrl: string }
    }): void
  }
}

declare global {
  interface Window {
    Paddle?: Paddle
  }
}

let loading: Promise<Paddle> | undefined

// Paddle.js loads on the first checkout only; the token and environment are fixed per deployment.
function load(token: string, environment: Checkout['environment']) {
  loading ??= new Promise<Paddle>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = 'https://cdn.paddle.com/paddle/v2/paddle.js'
    script.onload = () => {
      const paddle = window.Paddle!
      if (environment === 'sandbox') paddle.Environment.set('sandbox')
      paddle.Initialize({ token })
      resolve(paddle)
    }
    script.onerror = () => {
      script.remove()
      loading = undefined
      reject(new Error('Paddle.js failed to load'))
    }
    document.head.append(script)
  })
  return loading
}

export async function openCheckout(c: Checkout, successUrl: string) {
  const paddle = await load(c.client_token, c.environment)
  paddle.Checkout.open({
    transactionId: c.transaction_id,
    customer: { email: c.email },
    settings: { displayMode: 'overlay', successUrl },
  })
}
