// 2900 usd → "$29", 149900 inr → "₹1,499", 2950 usd → "$29.50". Amounts are in the currency's smallest unit.
// `locale` is for tests.
export function formatMoney(amount: number, currency: string, locale?: string): string {
  const digits = new Intl.NumberFormat(locale, { style: 'currency', currency }).resolvedOptions().maximumFractionDigits ?? 2
  const value = amount / 10 ** digits
  return new Intl.NumberFormat(locale, { style: 'currency', currency, ...(Number.isInteger(value) && { minimumFractionDigits: 0, maximumFractionDigits: 0 }) }).format(value)
}
