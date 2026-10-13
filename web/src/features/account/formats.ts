import { formatDateTime } from "@/lib/date-format"

/** Account timestamps arrive as Unix seconds; zero means "never happened". */
export function formatEpoch(seconds: number): string {
  return formatDateTime(new Date(seconds * 1000).toISOString())
}
