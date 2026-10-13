/** Exact rational arithmetic. Culinary amounts never pass through a float. */
export interface Rational {
  numerator: bigint
  denominator: bigint
}

function greatestCommonDivisor(left: bigint, right: bigint): bigint {
  let a = left < 0n ? -left : left
  let b = right < 0n ? -right : right
  while (b) [a, b] = [b, a % b]
  return a || 1n
}

export function rational(numerator: bigint, denominator = 1n): Rational {
  if (denominator === 0n) throw new Error("a rational cannot have a zero denominator")
  const sign = denominator < 0n ? -1n : 1n
  const divisor = greatestCommonDivisor(numerator, denominator)
  return { numerator: (sign * numerator) / divisor, denominator: (sign * denominator) / divisor }
}

/** Reads the canonical `n` or `n/d` form the API stores. */
export function parseRational(value: string): Rational | undefined {
  if (value.length > 128) return undefined
  const trimmed = value.trim()
  if (!/^-?\d+(\/\d+)?$/.test(trimmed)) return undefined
  const [numerator, denominator] = trimmed.split("/")
  const bottom = denominator ? BigInt(denominator) : 1n
  if (bottom === 0n) return undefined
  return rational(BigInt(numerator), bottom)
}

export function formatRational(value: Rational): string {
  return value.denominator === 1n
    ? String(value.numerator)
    : `${value.numerator}/${value.denominator}`
}

export const add = (left: Rational, right: Rational): Rational =>
  rational(
    left.numerator * right.denominator + right.numerator * left.denominator,
    left.denominator * right.denominator,
  )

export const multiply = (left: Rational, right: Rational): Rational =>
  rational(left.numerator * right.numerator, left.denominator * right.denominator)

export const divide = (left: Rational, right: Rational): Rational =>
  rational(left.numerator * right.denominator, left.denominator * right.numerator)

export const isZero = (value: Rational): boolean => value.numerator === 0n

export const toNumber = (value: Rational): number =>
  Number(value.numerator) / Number(value.denominator)

export const compare = (left: Rational, right: Rational): number => {
  const difference = left.numerator * right.denominator - right.numerator * left.denominator
  return difference === 0n ? 0 : difference > 0n ? 1 : -1
}

/** Rounds to the nearest multiple of `step`, halves away from zero. */
export function roundToStep(value: Rational, step: Rational): Rational {
  const quotient = divide(value, step)
  const doubled = quotient.numerator * 2n
  const negative = doubled < 0n
  const magnitude = negative ? -doubled : doubled
  let whole = (magnitude + quotient.denominator) / (quotient.denominator * 2n)
  if (negative) whole = -whole
  return multiply(rational(whole), step)
}
