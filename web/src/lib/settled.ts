import type { Resource } from "solid-js"

/**
 * A resource's last loaded value, read without suspending. Reading a resource
 * that is fetching suspends the nearest boundary, which is the route, so a
 * fetch started by typing or clicking would blank the page behind it.
 */
export function settled<T>(resource: Resource<T>): T | undefined {
  return resource.state === "ready" || resource.state === "refreshing" ? resource.latest : undefined
}
