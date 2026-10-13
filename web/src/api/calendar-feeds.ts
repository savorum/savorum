import { m } from "../lib/messages.ts"
import { client, expectOk, unwrap } from "./client"
import type { components } from "./schema"

export type CalendarFeed = components["schemas"]["CalendarFeed"]
export type CalendarFeedSecret = components["schemas"]["CalendarFeedSecret"]

export async function listCalendarFeeds(): Promise<CalendarFeed[]> {
  return (
    unwrap(await client.GET("/api/v1/calendar-feeds"), m.plan_calendar_feeds_load_failed()).items ??
    []
  )
}

export async function createCalendarFeed(name: string): Promise<CalendarFeedSecret> {
  return unwrap(
    await client.POST("/api/v1/calendar-feeds", { body: { name } }),
    m.plan_calendar_feed_failed(),
  )
}

export async function rotateCalendarFeed(id: string): Promise<CalendarFeedSecret> {
  return unwrap(
    await client.POST("/api/v1/calendar-feeds/{id}/rotate", { params: { path: { id } } }),
    m.plan_calendar_feed_failed(),
  )
}

export async function deleteCalendarFeed(id: string): Promise<void> {
  expectOk(
    await client.DELETE("/api/v1/calendar-feeds/{id}", { params: { path: { id } } }),
    m.plan_calendar_feed_failed(),
  )
}

/** The subscription address, absolute even when the instance has no public URL set. */
export function feedAddress(secret: CalendarFeedSecret): string {
  return secret.url || new URL(secret.path, window.location.origin).toString()
}
