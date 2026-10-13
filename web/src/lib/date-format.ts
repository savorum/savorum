import { presentationContext } from "./locale-format.ts"

/** Today in the account timezone, as a canonical calendar date. */
export function calendarDate(value = new Date()): string {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: presentationContext().timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(value)
  const part = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((entry) => entry.type === type)!.value
  return `${part("year")}-${part("month")}-${part("day")}`
}

/** Calendar-only dates have no timezone; timestamps do. */
export function formatDate(value: string): string {
  const { locale, timeZone } = presentationContext()
  const calendarOnly = /^\d{4}-\d{2}-\d{2}$/.test(value)
  const date = new Date(calendarOnly ? `${value}T12:00:00Z` : value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeZone: calendarOnly ? "UTC" : timeZone,
  }).format(date)
}

export function formatRelativeDate(value: string): string {
  const then = Date.parse(`${value}T00:00:00Z`)
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value) || Number.isNaN(then)) return value
  const now = Date.parse(`${calendarDate()}T00:00:00Z`)
  const days = Math.round((now - then) / 86_400_000)
  const relative = new Intl.RelativeTimeFormat(presentationContext().locale, { numeric: "auto" })
  if (Math.abs(days) < 7) return relative.format(-days, "day")
  if (Math.abs(days) < 30) return relative.format(-Math.round(days / 7), "week")
  if (Math.abs(days) < 365) return relative.format(-Math.round(days / 30), "month")
  return relative.format(-Math.round(days / 365), "year")
}

export function formatDateTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const { locale, timeZone } = presentationContext()
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone,
  }).format(date)
}

/**
 * A source's own date-time, read the way the place that wrote it would read
 * it. The value carries its own UTC offset; moving it into the reader's
 * timezone would have the source saying something it did not say.
 */
export function formatSourceDateTime(value: string): string {
  const clock = sourceClock(value)
  if (!clock) return value
  return new Intl.DateTimeFormat(presentationContext().locale, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(clock)
}

/** The day a source dated something, in the source's own terms. */
export function formatSourceDate(value: string): string {
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) return formatDate(value)
  const clock = sourceClock(value)
  if (!clock) return value
  return new Intl.DateTimeFormat(presentationContext().locale, {
    dateStyle: "medium",
    timeZone: "UTC",
  }).format(clock)
}

/** The source's wall clock, expressed as a UTC instant. */
function sourceClock(value: string): Date | undefined {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return undefined
  const offset = /([+-])(\d{2}):?(\d{2})$/.exec(value)
  const minutes = offset
    ? (offset[1] === "-" ? -1 : 1) * (Number(offset[2]) * 60 + Number(offset[3]))
    : 0
  return new Date(date.getTime() + minutes * 60_000)
}

/** A day the browser built in local time, in the interface language. */
export function formatLocalDay(day: Date, options: Intl.DateTimeFormatOptions): string {
  return new Intl.DateTimeFormat(presentationContext().locale, options).format(day)
}

/** The span of local days from `start` to `end`, sharing what they have in common. */
export function formatLocalDays(
  start: Date,
  end: Date,
  options: Intl.DateTimeFormatOptions,
): string {
  return new Intl.DateTimeFormat(presentationContext().locale, options).formatRange(start, end)
}

/** A calendar-only date such as a meal's day, in the interface language. */
export function formatCalendarDay(value: string, options: Intl.DateTimeFormatOptions): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return value
  return new Intl.DateTimeFormat(presentationContext().locale, {
    ...options,
    timeZone: "UTC",
  }).format(new Date(`${value}T12:00:00Z`))
}
