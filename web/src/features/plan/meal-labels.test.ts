import { expect, test } from "vitest"

import { configurePresentation } from "@/lib/locale-format"

import { scaled } from "./meal-labels"

test("a dish names its batch in words when it is not the recipe as written", () => {
  expect(scaled({ display_name: "Soup" })).toBe("Soup")
  expect(scaled({ display_name: "Soup", scale: "1" })).toBe("Soup")
  expect(scaled({ display_name: "Soup", scale: "1/2" })).toBe("Soup (½ batch)")
  expect(scaled({ display_name: "Soup", scale: "1/3" })).toBe("Soup (⅓ batch)")
  expect(scaled({ display_name: "Soup", scale: "2/5" })).toBe("Soup (2/5 batch)")
  expect(scaled({ display_name: "Soup", scale: "999/1000" })).toBe("Soup (999/1000 batch)")
  expect(scaled({ display_name: "Soup", scale: "1001/1000" })).toBe("Soup (1001/1000 batches)")
  expect(scaled({ display_name: "Soup", scale: "2" })).toBe("Soup (2 batches)")
  expect(scaled({ display_name: "Soup", scale: "3/2" })).toBe("Soup (1½ batches)")
})

test("batch fractions use the preferred digits without falling back to localized decimals", () => {
  configurePresentation(() => ({ locale: "ar-u-nu-arab", timeZone: "UTC" }))
  try {
    expect(scaled({ display_name: "Soup", scale: "2/5" })).toBe("Soup (٢/٥ batch)")
    expect(scaled({ display_name: "Soup", scale: "1/10000" })).toBe("Soup (١/١٠٠٠٠ batch)")
    expect(scaled({ display_name: "Soup", scale: "999/1000" })).toBe("Soup (٩٩٩/١٠٠٠ batch)")
  } finally {
    configurePresentation(() => ({ locale: "en", timeZone: "UTC" }))
  }
})
