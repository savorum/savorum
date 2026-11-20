import { afterEach, expect, test, vi } from "vitest"

import { account, refreshAccount } from "./account-state"

afterEach(() => sessionStorage.clear())

test("a failed joined-library selection still loads the account", async () => {
  sessionStorage.setItem("savorum.joined-library", "joined")
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = input instanceof Request ? input.url : input.toString()
      if (url.includes("/api/v1/account/library")) return new Response(null, { status: 503 })
      if (url.includes("/api/v1/account"))
        return Response.json({
          auth_enabled: true,
          user: { id: "u1", display_name: "Alex", instance_admin: false },
          libraries: [
            { id: "home", name: "Home", role: "owner" },
            { id: "joined", name: "Joined", role: "viewer" },
          ],
          csrf_token: "token",
          active_library_id: "home",
        })
      if (url.includes("/preferences"))
        return Response.json({ effective: {}, overrides: {}, revision: 1 })
      return new Response(null, { status: 404 })
    }),
  )

  await refreshAccount()

  expect(account()?.user?.id).toBe("u1")
  expect(account()?.active_library_id).toBe("home")
  expect(sessionStorage.getItem("savorum.joined-library")).toBeNull()
})
