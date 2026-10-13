import { expect, test, vi } from "vitest"

import { preview } from "@/test/import-fixtures"

import { client } from "./client"
import { startImportJob } from "./import-jobs"

test("job start normalizes its remaining preview through the shared API contract", async () => {
  vi.spyOn(client, "POST").mockResolvedValue({
    data: {
      job: { id: "job" },
      remaining: {
        ...preview(),
        preview_revision: 0,
        items: null,
        resolution_groups: null,
        resolution_profiles: null,
      },
    },
    response: new Response(),
  } as unknown as Awaited<ReturnType<typeof client.POST>>)
  const result = await startImportJob("preview", { itemIDs: ["one"] })
  expect(result.job.id).toBe("job")
  expect(result.remaining).toEqual({
    ...preview(),
    preview_revision: 1,
    items: [],
    resolution_groups: [],
    resolution_profiles: [],
  })
})
