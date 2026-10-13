import { account } from "@/features/account/account-state"
import { measurementSystem, temperatureSystem, personalPreferences } from "@/lib/preferences"

import type { CookingDisplay, CookingScope } from "./cooking-session"

export function cookingOwner(): CookingScope | undefined {
  const owner = account()
  if (
    typeof window === "undefined" ||
    !owner?.active_library_id ||
    (owner.auth_enabled && !owner.user)
  )
    return undefined
  return {
    instance: window.location.origin,
    principal: owner.user?.id ?? "local",
    library: owner.active_library_id,
  }
}

export function cookingDisplay(): CookingDisplay {
  return {
    system: measurementSystem(),
    temperatures: temperatureSystem(),
    microwaveWatts: personalPreferences().microwave_output_watts ?? 0,
  }
}
