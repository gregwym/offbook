// AmountDisplay is the ONLY allowed place to format monetary values in the
// UI. Money arrives as a decimal string (NUMERIC(30,18)) — never convert to
// Number before display: that loses precision at crypto scale (BTC has 8
// places, ETH has 18). We slice the string and let Intl.NumberFormat handle
// the integer + fractional parts separately.

type Props = {
  amount: string | undefined | null
  currency?: string
  // Override display precision; default is currency-default (2 for fiat).
  fractionDigits?: number
  // Apply red text when negative (handy for spending columns).
  signed?: boolean
  className?: string
}

const DEFAULT_FRACTION: Record<string, number> = {
  USD: 2, EUR: 2, GBP: 2, JPY: 0, CAD: 2, AUD: 2,
  BTC: 8, ETH: 6,
}

export function AmountDisplay({ amount, currency = 'USD', fractionDigits, signed, className }: Props) {
  if (amount === undefined || amount === null || amount === '') {
    return <span className={className}>—</span>
  }
  const fraction = fractionDigits ?? DEFAULT_FRACTION[currency] ?? 2
  const negative = amount.trim().startsWith('-')
  const text = formatDecimal(amount, fraction, currency)
  const color = signed && negative ? 'text-red-700' : signed && !negative ? 'text-emerald-700' : ''
  return <span className={[color, className ?? ''].join(' ').trim()}>{text}</span>
}

function formatDecimal(value: string, fraction: number, currency: string): string {
  const trimmed = value.trim()
  const sign = trimmed.startsWith('-') ? '-' : ''
  const abs = sign ? trimmed.slice(1) : trimmed
  const [intPart = '0', fracRaw = ''] = abs.split('.')

  // Group integer part using locale separators via Intl.
  const intFormatted = new Intl.NumberFormat(undefined, { useGrouping: true }).format(
    BigInt(intPart || '0'),
  )

  let fracOut = ''
  if (fraction > 0) {
    const padded = (fracRaw + '0'.repeat(fraction)).slice(0, fraction)
    fracOut = padded.length > 0 ? '.' + padded : ''
  }

  // Currency suffix keeps the format simple and unambiguous across symbols.
  return `${sign}${intFormatted}${fracOut} ${currency}`
}
