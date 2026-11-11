// An amount in the currency's minor units (cents), e.g. "$12" or "$12.50".
export const money = (minor: number, currency: string) => {
  const f = new Intl.NumberFormat(undefined, { style: 'currency', currency, trailingZeroDisplay: 'stripIfInteger' } as Intl.NumberFormatOptions)
  return f.format(minor / 10 ** (f.resolvedOptions().maximumFractionDigits ?? 2))
}
