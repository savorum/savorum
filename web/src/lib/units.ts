import { localizedUnitLabel, localizedUnitTight } from "./localized-units.ts"
import {
  type Rational,
  add,
  compare,
  divide,
  multiply,
  parseRational,
  rational,
  roundToStep,
  toNumber,
} from "./rational.ts"
import {
  type LadderRung,
  type MeasurementSystem,
  type RegistryUnit,
  type UnitConvention,
  type UnitId,
  type UnitLabel,
  measurementLadders,
  unitLabels,
  unitRegistry,
  volumePresentation,
} from "./unit-registry.generated.ts"

export type { MeasurementSystem, UnitConvention, UnitId }

export type Dimension = "mass" | "volume" | "length" | "temperature" | "count"

interface UnitDefinition extends UnitLabel {
  dimension: Dimension
  /** Amount of the dimension's base unit in one of this unit. */
  factor?: Rational
  /** Added after scaling, for temperature scales that do not share an origin. */
  offset?: Rational
  /** Measuring convention this unit belongs to, when it has one. */
  convention?: UnitConvention
  /** False when the registry definition is itself a culinary approximation. */
  exact: boolean
}

/** Reads a canonical registry amount, which is generated and never typed. */
function registryAmount(value: string): Rational {
  const parsed = parseRational(value)
  if (!parsed) throw new Error(`the unit registry holds an unreadable amount: ${value}`)
  return parsed
}

function defineUnit(entry: RegistryUnit): UnitDefinition {
  return {
    ...unitLabels[entry.id],
    // A count or a container has no dimension of its own; it still scales.
    dimension: entry.dimension ?? "count",
    factor: entry.factor === undefined ? undefined : registryAmount(entry.factor),
    offset: entry.offset === undefined ? undefined : registryAmount(entry.offset),
    convention: entry.convention,
    exact: entry.exact ?? true,
  }
}

const units: Record<string, UnitDefinition> = Object.fromEntries(
  unitRegistry.map((entry) => [entry.id, defineUnit(entry)]),
)

export const knownUnits: string[] = Object.keys(units)

export function unitLabel(unit: string | undefined, amount?: Rational): string {
  if (!unit) return ""
  const localized = localizedUnitLabel(unit, amount)
  if (localized !== undefined) return localized
  const definition = units[unit]
  if (!definition) return unit
  const singular = amount !== undefined && compare(amount, rational(1n)) <= 0
  return singular ? definition.one : definition.many
}

export function unitIsTight(unit: string | undefined): boolean {
  if (unit) {
    const localized = localizedUnitTight(unit)
    if (localized !== undefined) return localized
  }
  return Boolean(unit && units[unit]?.tight)
}

/** Full registry name, used only where precision genuinely matters. */
export function unitDetail(unit: string | undefined): string | undefined {
  if (!unit || !units[unit]) return unit
  const convention = units[unit].convention
  const names: Record<UnitConvention, string> = {
    metric: "metric",
    us: "United States",
    au: "Australian",
    imperial: "imperial",
  }
  return convention ? `${names[convention]} ${units[unit].many || unit}` : undefined
}

/**
 * The measure a unit is one interpretation of: the first two segments of its
 * identity. `volume.cup.us.customary`, `volume.cup.au` and
 * `volume.cup.metric.culinary` are three answers to one question.
 */
function unitFamily(unitID: string): string {
  const parts = unitID.split(".")
  return parts.length < 2 ? unitID : `${parts[0]}.${parts[1]}`
}

const contestedFamilies = new Set(
  (() => {
    const counts = new Map<string, number>()
    for (const id of Object.keys(units)) {
      const family = unitFamily(id)
      counts.set(family, (counts.get(family) ?? 0) + 1)
    }
    return [...counts].filter(([, count]) => count > 1).map(([family]) => family)
  })(),
)

/**
 * Reports whether an identity is one of several competing answers for the same
 * written measure (a cup, not a gram), and so one a person may re-decide.
 */
export function isRegionallyAmbiguous(unitID: string | undefined): boolean {
  if (!unitID) return false
  const unit = units[unitID]
  return Boolean(unit?.convention) && contestedFamilies.has(unitFamily(unitID))
}

export function measurementConvention(unitIDs: string[]): UnitConvention | undefined {
  const conventions = new Set<UnitConvention>()
  for (const unit of unitIDs) {
    const definition = units[unit]
    if (!definition || definition.dimension !== "volume") continue
    if (definition.convention) conventions.add(definition.convention)
  }
  return conventions.size === 1 ? conventions.values().next().value : undefined
}

/**
 * Reports whether a physical unit is already written in the convention a
 * requested display system uses for that dimension. Undefined means the unit
 * is unitless or nonphysical and therefore neither favors nor opposes a
 * presentation system.
 */
export function unitMatchesMeasurementSystem(
  unit: string | undefined,
  system: MeasurementSystem,
): boolean | undefined {
  if (!unit || system === "original") return undefined
  const definition = units[unit]
  if (!definition) return false
  if (!definition.factor) return undefined
  const target = ladders[system][definition.dimension]?.[0]
  if (!target) return false
  return definition.convention === units[target.unit]?.convention
}

function toBase(amount: Rational, unit: string): Rational | undefined {
  const definition = units[unit]
  if (!definition?.factor) return undefined
  const scaled = multiply(amount, definition.factor)
  return definition.offset ? add(scaled, definition.offset) : scaled
}

function fromBase(base: Rational, unit: string): Rational | undefined {
  const definition = units[unit]
  if (!definition?.factor) return undefined
  const shifted = definition.offset ? add(base, multiply(definition.offset, rational(-1n))) : base
  return divide(shifted, definition.factor)
}

/**
 * The rounding policy, read from the generated registry so screens round the
 * same way as the printed document.
 */
interface Ladder {
  unit: UnitId
  ceiling?: Rational
  step: Rational
}

function readLadder(rungs: readonly LadderRung[]): Ladder[] {
  return rungs.map((rung) => ({
    unit: rung.unit,
    ceiling: rung.ceiling === undefined ? undefined : registryAmount(rung.ceiling),
    step: registryAmount(rung.step),
  }))
}

const ladders: Record<MeasurementSystem, Partial<Record<Dimension, Ladder[]>>> = Object.fromEntries(
  Object.entries(measurementLadders).map(([system, byDimension]) => [
    system,
    Object.fromEntries(
      Object.entries(byDimension).map(([dimension, rungs]) => [
        dimension,
        readLadder(rungs as readonly LadderRung[]),
      ]),
    ),
  ]),
) as Record<MeasurementSystem, Partial<Record<Dimension, Ladder[]>>>

/** Chooses the rung a base amount belongs on, in the ladder's own measures. */
function ladderStep(ladder: Ladder[], base: Rational): Ladder {
  return (
    ladder.find((entry) => {
      if (!entry.ceiling) return true
      const ceiling = toBase(entry.ceiling, entry.unit)
      return ceiling !== undefined && compare(base, ceiling) < 0
    }) ?? ladder.at(-1)!
  )
}

export interface ConvertedAmount {
  amount: Rational
  unit: string
  parts?: readonly { amount: Rational; unit: string }[]
  /** True when the shown amount was rounded to a practical measure. */
  approximate: boolean
}

interface VolumeCandidate {
  amount: Rational
  unit: string
  error: Rational
  score: number
  parts?: readonly { amount: Rational; unit: string }[]
}

function absoluteDifference(left: Rational, right: Rational): Rational {
  const difference = add(left, multiply(right, rational(-1n)))
  return difference.numerator < 0n ? multiply(difference, rational(-1n)) : difference
}

function relativeError(error: Rational, value: Rational): Rational {
  if (value.numerator === 0n) return rational(0n)
  const magnitude = value.numerator < 0n ? multiply(value, rational(-1n)) : value
  return divide(error, magnitude)
}

/**
 * The measures the rung at this index offers, falling back to the coarsest
 * rung the policy names for a family with more rungs than it enumerates.
 */
function rungFractions(index: number): readonly string[] {
  return volumePresentation.fractions[index] ?? volumePresentation.fractions.at(-1)!
}

/** What the larger measure of a two-measure plan may read as, at this rung. */
function compoundRungFractions(index: number): readonly string[] {
  return volumePresentation.compoundFractions[index] ?? volumePresentation.compoundFractions.at(-1)!
}

/** The step a compound plan's smaller measure is counted out in. */
function remainderStep(index: number): Rational | undefined {
  return parseRational(
    volumePresentation.remainderSteps[index] ?? volumePresentation.remainderSteps.at(-1)!,
  )
}

function floorToStep(value: Rational, step: Rational): Rational {
  const quotient = divide(value, step)
  return multiply(rational(quotient.numerator / quotient.denominator), step)
}

function volumeScore(value: Rational, unitIndex: number): number {
  const magnitude = value.numerator < 0n ? multiply(value, rational(-1n)) : value
  const whole = magnitude.numerator / magnitude.denominator
  const operations = Number(whole)
  if (magnitude.denominator === 1n) return operations
  if (unitIndex === 0) {
    if (magnitude.denominator === 2n) return operations + 1
    if (magnitude.denominator === 4n) {
      return operations + (magnitude.numerator % magnitude.denominator === 3n ? 2 : 1)
    }
    if (magnitude.denominator === 3n) return operations + 4
    return operations + 4
  }
  if (unitIndex === 1) {
    if (magnitude.denominator === 2n) return operations === 0 ? 2 : operations + 1
    return operations + 5
  }
  if (unitIndex === 2) {
    return operations + ([2n, 3n, 4n].includes(magnitude.denominator) ? 1 : 5)
  }
  return operations
}

function volumeUnits(unit: string, system: MeasurementSystem): string[] | undefined {
  const definition = units[unit]
  const convention = system === "original" ? definition?.convention : system
  if (convention === "metric") {
    return ["volume.teaspoon.metric.culinary"]
  }
  if (convention === "au") {
    return ["volume.teaspoon.au", "volume.tablespoon.au", "volume.cup.au"]
  }
  if (convention !== "us") return undefined
  // US legal measures are exact labeling identities, not a kitchen family.
  // Each stays itself as written rather than borrowing customary siblings.
  if (system === "original" && unit.endsWith(".us.legal")) {
    return [unit]
  }
  const culinary = system === "original" && unit.endsWith(".us.culinary")
  const variant = culinary ? "culinary" : "customary"
  return [
    `volume.teaspoon.us.${variant}`,
    `volume.tablespoon.us.${variant}`,
    `volume.cup.us.${variant}`,
  ]
}

/**
 * How much of the larger measure a compound plan might use: every whole number
 * of it that fits, and the largest amount each fraction the rung offers as a
 * graduated vessel can reach, so five-eighths of a cup can be half a cup and
 * two tablespoons.
 */
function compoundLargerAmounts(actualInLarger: Rational, rung: number): Rational[] {
  const amounts: Rational[] = []
  const add = (candidate: Rational) => {
    if (candidate.numerator <= 0n || compare(candidate, actualInLarger) > 0) return
    if (amounts.some((existing) => compare(existing, candidate) === 0)) return
    amounts.push(candidate)
  }
  const whole = actualInLarger.numerator / actualInLarger.denominator
  for (let count = 1n; count <= whole && count <= 64n; count += 1n) add(rational(count))
  for (const fraction of compoundRungFractions(rung)) {
    const step = parseRational(fraction)
    if (step && step.numerator > 0n) add(floorToStep(actualInLarger, step))
  }
  return amounts
}

/** Chooses a kitchen-scale volume expression after exact scaling. */
function presentFriendlyVolume(
  amount: Rational,
  unit: string,
  system: MeasurementSystem,
): ConvertedAmount | undefined {
  const definition = units[unit]
  if (!definition?.factor || definition.dimension !== "volume") return undefined
  const candidates = volumeUnits(unit, system)
  if (!candidates) return undefined
  const base = toBase(amount, unit)
  const teaspoon = candidates[0]
  const actual = base && fromBase(base, teaspoon)
  const maxRelative = parseRational(volumePresentation.maxRelativeError)
  const maxAbsolute = parseRational(volumePresentation.maxTeaspoonError)
  const errorWeight = parseRational(volumePresentation.relativeErrorWeight)
  const minimumGain = parseRational(volumePresentation.minimumSimplicityGain)
  if (!base || !actual || !maxRelative || !maxAbsolute || !errorWeight || !minimumGain)
    return undefined
  const convention = system === "original" ? definition.convention : system
  if (convention === "metric" && compare(actual, rational(6n)) > 0) return undefined
  const metricRelative = convention === "metric" ? rational(1n, 8n) : maxRelative

  let bestExact: VolumeCandidate | undefined
  let bestApproximate: VolumeCandidate | undefined
  for (const [unitIndex, candidateUnit] of candidates.entries()) {
    const converted = fromBase(base, candidateUnit)
    if (!converted) continue
    for (const fraction of rungFractions(unitIndex)) {
      const step = parseRational(fraction)
      if (!step) continue
      const rounded = roundToStep(converted, step)
      if (rounded.numerator <= 0n) continue
      const candidateBase = toBase(rounded, candidateUnit)
      const candidateTeaspoons = candidateBase && fromBase(candidateBase, teaspoon)
      if (!candidateTeaspoons) continue
      const error = absoluteDifference(candidateTeaspoons, actual)
      if (
        compare(error, maxAbsolute) > 0 ||
        compare(relativeError(error, actual), metricRelative) > 0
      )
        continue
      const candidate: VolumeCandidate = {
        amount: rounded,
        unit: candidateUnit,
        error,
        score:
          volumeScore(rounded, unitIndex) +
          toNumber(relativeError(error, actual)) * toNumber(errorWeight),
      }
      const best = error.numerator === 0n ? bestExact : bestApproximate
      if (
        !best ||
        candidate.score < best.score ||
        (candidate.score === best.score && compare(candidate.error, best.error) < 0)
      ) {
        if (error.numerator === 0n) bestExact = candidate
        else bestApproximate = candidate
      }
    }
  }
  // The plans a cook would actually count out: so many of a larger measure and
  // a remainder in the next smaller one. Every adjacent pair in the family is
  // tried, so nine tablespoons can be half a cup and one tablespoon.
  for (let larger = 1; actual.numerator > 0n && larger < candidates.length; larger += 1) {
    const smaller = larger - 1
    const largerInTeaspoons = fromBase(toBase(rational(1n), candidates[larger])!, teaspoon)
    const smallerInTeaspoons = fromBase(toBase(rational(1n), candidates[smaller])!, teaspoon)
    const step = remainderStep(smaller)
    if (!largerInTeaspoons || !smallerInTeaspoons || !step) continue
    const actualInLarger = divide(actual, largerInTeaspoons)
    for (const whole of compoundLargerAmounts(actualInLarger, larger)) {
      const used = multiply(whole, largerInTeaspoons)
      const exact = divide(add(actual, multiply(used, rational(-1n))), smallerInTeaspoons)
      const nearest = roundToStep(exact, step)
      const lower = floorToStep(exact, step)
      const upper = compare(lower, exact) === 0 ? lower : add(lower, step)
      for (const remainder of [nearest, lower, upper]) {
        if (
          remainder.numerator <= 0n ||
          (remainder !== nearest && compare(remainder, nearest) === 0)
        )
          continue
        const measured = add(used, multiply(remainder, smallerInTeaspoons))
        const error = absoluteDifference(measured, actual)
        const relative = relativeError(error, actual)
        if (compare(error, maxAbsolute) > 0 || compare(relative, metricRelative) > 0) continue
        const candidate: VolumeCandidate = {
          amount: actual,
          unit: teaspoon,
          error,
          score:
            volumeScore(whole, larger) +
            volumeScore(remainder, smaller) +
            toNumber(relative) * toNumber(errorWeight),
          parts: [
            { amount: whole, unit: candidates[larger] },
            { amount: remainder, unit: candidates[smaller] },
          ],
        }
        const best = error.numerator === 0n ? bestExact : bestApproximate
        if (
          !best ||
          candidate.score < best.score ||
          (candidate.score === best.score && compare(candidate.error, best.error) < 0)
        ) {
          if (error.numerator === 0n) bestExact = candidate
          else bestApproximate = candidate
        }
      }
    }
  }
  const best =
    bestApproximate &&
    (!bestExact || bestApproximate.score + toNumber(minimumGain) < bestExact.score)
      ? bestApproximate
      : bestExact
  if (best) {
    return {
      amount: best.amount,
      unit: best.unit,
      parts: best.parts,
      approximate:
        best.error.numerator !== 0n || !definition.exact || !(units[best.unit]?.exact ?? true),
    }
  }
  return {
    amount: actual,
    unit: teaspoon,
    approximate: !definition.exact || !(units[teaspoon]?.exact ?? true),
  }
}

/**
 * Presents an amount in the reader's preferred measurement system. Conversion
 * is presentation only; the stored amount is never changed.
 */
export function convertAmount(
  amount: Rational,
  unit: string | undefined,
  system: MeasurementSystem,
  normalize = system !== "original",
): ConvertedAmount | undefined {
  if (!unit) return undefined
  const definition = units[unit]
  if (!definition) return undefined
  // Variants inside one convention remain the authored measure, rather than
  // an exact but absurd pile of tablespoons and teaspoons.
  if (system === "us" && unit.includes(".us.culinary")) {
    return { amount, unit, approximate: false }
  }
  const friendly = normalize ? presentFriendlyVolume(amount, unit, system) : undefined
  if (friendly) return friendly
  if (system === "original" || !definition.factor) return { amount, unit, approximate: false }
  const ladder = ladders[system][definition.dimension]
  if (!ladder) return { amount, unit, approximate: false }
  const base = toBase(amount, unit)
  if (!base) return { amount, unit, approximate: false }
  const step = ladderStep(ladder, base)
  const converted = fromBase(base, step.unit)
  if (!converted) return { amount, unit, approximate: false }
  const rounded = roundToStep(converted, step.step)
  const unrounded = compare(rounded, converted) === 0
  const usable = toNumber(rounded) === 0 ? converted : rounded
  // A registry definition that is itself a culinary approximation cannot
  // produce an exact projection. Matches the server.
  const faithful = definition.exact && (units[step.unit]?.exact ?? true)
  return {
    amount: usable,
    unit: step.unit,
    approximate: !unrounded || !faithful,
  }
}

export function scaleRational(value: Rational, factor: Rational): Rational {
  return multiply(value, factor)
}

export { parseRational }
