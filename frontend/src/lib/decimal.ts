// Exact decimal-string arithmetic for money deltas. Money arrives as decimal
// strings (NUMERIC(30,18)); Number math loses precision at crypto scale.

const SCALE = 18

function toScaled(s: string): bigint {
  const t = (s ?? '').trim()
  if (!/^[+-]?\d*\.?\d*$/.test(t) || t === '' || t === '.' ) return 0n
  const neg = t.startsWith('-')
  const abs = t.replace(/^[+-]/, '')
  const [i = '0', f = ''] = abs.split('.')
  const scaled = BigInt(i || '0') * 10n ** BigInt(SCALE) + BigInt((f + '0'.repeat(SCALE)).slice(0, SCALE))
  return neg ? -scaled : scaled
}

// subDecimal returns a - b as a plain decimal string with trailing zeros trimmed.
export function subDecimal(a: string, b: string): string {
  const d = toScaled(a) - toScaled(b)
  const neg = d < 0n
  const abs = neg ? -d : d
  const digits = abs.toString().padStart(SCALE + 1, '0')
  const int = digits.slice(0, -SCALE)
  const frac = digits.slice(-SCALE).replace(/0+$/, '')
  return `${neg ? '-' : ''}${int}${frac ? '.' + frac : ''}`
}

// addDecimal returns a + b as a plain decimal string.
export function addDecimal(a: string, b: string): string {
  return subDecimal(a, subDecimal('0', b))
}

// absDecimal returns |a| as a plain decimal string.
export function absDecimal(a: string): string {
  const t = (a ?? '').trim()
  return t.startsWith('-') ? subDecimal('0', t) : subDecimal(t, '0')
}
