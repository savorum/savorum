import { cleanup, fireEvent, render } from "@solidjs/testing-library"
import { afterEach, expect, test, vi } from "vitest"

import type { Recipe } from "@/api/client"
import { applyPreferences, resetUserPreferences } from "@/lib/preferences"
import { rational } from "@/lib/rational"

import { CookingMode } from "./cooking-mode"
import { newCookingSession, updateCookingSession } from "./cooking-session"
import type { CookingSession } from "./cooking-session"
import { CookingWakeLock } from "./wake-lock"

afterEach(() => {
  cleanup()
  resetUserPreferences()
})

const recipe: Recipe = {
  schema_version: 1,
  id: "oven-recipe",
  title: "Tray Bake",
  ingredient_sections: [
    {
      id: "ingredients-1",
      ingredients: [
        {
          id: "flour",
          name: "plain flour",
          quantity: { kind: "simple", amount: { value: "300" }, unit: "mass.gram" },
        },
      ],
    },
  ],
  instruction_sections: [
    {
      id: "instructions-1",
      steps: [
        {
          id: "bake",
          text: "Bake until the centre springs back.",
          temperatures: [
            { amount: { value: "180" }, unit: "temperature.celsius", purpose: { kind: "oven" } },
          ],
        },
      ],
    },
  ],
}

const session: CookingSession = {
  id: "session-1",
  userID: "local",
  recipeID: "oven-recipe",
  scale: "1",
  checkedIngredientIDs: [],
  completedStepIDs: [],
  keepAwake: false,
  startedAt: 0,
}

function cook() {
  return render(() => (
    <CookingMode
      recipe={recipe}
      entryID="entry-1"
      session={session}
      onSession={() => {}}
      onExit={() => {}}
      onStartOver={() => {}}
      onFinish={() => Promise.resolve()}
      finishing={false}
      wakeLock={new CookingWakeLock()}
    />
  ))
}

// The temperature preference is independent of the measurement region, so US
// amounts beside a Celsius oven is a supported combination rather than a
// contradiction to resolve toward one system.
test("cooking reads oven temperature in the temperature preference, not the measurement region", () => {
  applyPreferences({
    version: 1,
    revision: 1,
    overrides: { measurement: "us", temperature: "celsius" },
    effective: { measurement: "us", temperature: "celsius" },
  })
  const screen = cook()
  expect(screen.getByText("180°C oven")).toBeTruthy()
  expect(screen.queryByText(/°F/)).toBeNull()
  expect(screen.getAllByText(/10½ oz/).length).toBeGreaterThan(0)
})

test("cooking converts the oven temperature when the preference asks for Fahrenheit", () => {
  applyPreferences({
    version: 1,
    revision: 1,
    overrides: { measurement: "original", temperature: "fahrenheit" },
    effective: { measurement: "original", temperature: "fahrenheit" },
  })
  const screen = cook()
  expect(screen.getByText("355°F oven")).toBeTruthy()
})

test("going back counts the previous step as not done again", () => {
  const twoSteps: Recipe = {
    ...recipe,
    instruction_sections: [
      {
        id: "method",
        steps: [
          { id: "first", text: "Mix." },
          { id: "second", text: "Bake." },
        ],
      },
    ],
  }
  const stored = updateCookingSession(newCookingSession("two-steps", rational(1n), "first"), {
    completedStepIDs: ["first"],
    selectedStepID: "second",
  })
  const onSession = vi.fn()
  const screen = render(() => (
    <CookingMode
      recipe={twoSteps}
      entryID="entry-1"
      session={stored}
      onSession={onSession}
      onExit={() => {}}
      onStartOver={() => {}}
      onFinish={() => Promise.resolve()}
      finishing={false}
      wakeLock={new CookingWakeLock()}
    />
  ))
  fireEvent.click(screen.getByRole("button", { name: "Previous step" }))
  expect(onSession).toHaveBeenCalledWith(
    expect.objectContaining({ selectedStepID: "first", completedStepIDs: [] }),
  )
  expect(screen.queryByRole("button", { name: /Skip step|Mark started/ })).toBeNull()
})

test("an ingredient split between steps shows each step's own share", () => {
  const split: Recipe = {
    ...recipe,
    ingredient_sections: [
      {
        id: "ingredients-1",
        ingredients: [
          {
            id: "butter",
            name: "butter",
            quantity: { kind: "simple", amount: { value: "150" }, unit: "mass.gram" },
          },
        ],
      },
    ],
    instruction_sections: [
      {
        id: "method",
        steps: [
          {
            id: "first",
            text: "Cream most of the butter.",
            ingredient_uses: [
              {
                ingredient_ref: "butter",
                quantity: { kind: "simple", amount: { value: "100" }, unit: "mass.gram" },
              },
            ],
          },
          {
            id: "second",
            text: "Melt the rest to brush on top.",
            ingredient_uses: [
              {
                ingredient_ref: "butter",
                quantity: { kind: "simple", amount: { value: "50" }, unit: "mass.gram" },
              },
            ],
          },
        ],
      },
    ],
  }
  const at = (stepID: string) =>
    render(() => (
      <CookingMode
        recipe={split}
        entryID="entry-1"
        session={updateCookingSession(newCookingSession("split", rational(1n), "first"), {
          selectedStepID: stepID,
        })}
        onSession={() => {}}
        onExit={() => {}}
        onStartOver={() => {}}
        onFinish={() => Promise.resolve()}
        finishing={false}
        wakeLock={new CookingWakeLock()}
      />
    ))
  const first = at("first")
  expect(first.getByText("100 g butter")).toBeTruthy()
  expect(first.queryByText("150 g butter")).toBeNull()
  expect(first.queryByText(/Follow the step when it uses part of one/)).toBeNull()
  cleanup()
  expect(at("second").getByText("50 g butter")).toBeTruthy()
})
