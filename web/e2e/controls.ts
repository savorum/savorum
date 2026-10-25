import { expect, type Locator, type Page } from "@playwright/test"

/** Waits for focus restoration unless navigation removes the chooser. */
async function settled(chooser: Locator): Promise<void> {
  await expect(chooser).toHaveAttribute("data-state", "closed")
  const id = await chooser.getAttribute("id")
  if (!id) return
  await chooser.page().waitForFunction((triggerID) => {
    const trigger = document.getElementById(triggerID)
    return !trigger || document.activeElement === trigger
  }, id)
}

/**
 * Picks an option from one of the application's choosers by the value it
 * stores. The list must be opened before its options exist.
 */
export async function choose(chooser: Locator, value: string): Promise<void> {
  const list = await open(chooser)
  await list.locator(`[role="option"][data-value="${value}"]`).click()
  await settled(chooser)
}

/** Picks an option by the wording a reader sees, where the value is an id. */
export async function chooseNamed(chooser: Locator, label: string): Promise<void> {
  const list = await open(chooser)
  await list.getByRole("option", { name: label, exact: true }).click()
  await settled(chooser)
}

/** Scopes the portal list to its trigger because option labels can repeat. */
async function open(chooser: Locator): Promise<Locator> {
  await chooser.click()
  const listID = await chooser.getAttribute("aria-controls")
  return listID ? chooser.page().locator(`[id="${listID}"]`) : chooser.page().locator("body")
}

/** The chooser with this accessible name, anywhere on the page. */
export function chooser(page: Page, name: string): Locator {
  return page.getByRole("combobox", { name, exact: true })
}

/** The trigger button of a collapsible section, named by what it reads. */
export function section(page: Page, name: RegExp | string): Locator {
  return page.getByRole("button", { name })
}

/** Opens a collapsible section, having checked it was shut to begin with. */
export async function openSection(page: Page, name: RegExp | string): Promise<void> {
  const trigger = section(page, name)
  await expect(trigger).toHaveAttribute("aria-expanded", "false")
  await trigger.click()
  await expect(trigger).toHaveAttribute("aria-expanded", "true")
}

/** Retries selection if scrolling moved the menu before the click landed. */
export async function chooseAction(page: Page, menu: string, item: string): Promise<void> {
  const trigger = page.getByRole("button", { name: menu, exact: true })
  await trigger.click()
  await expect(async () => {
    const choice = page.getByRole("menuitem", { name: item, exact: true })
    if (await choice.isVisible()) await choice.click()
    await expect(trigger).toHaveAttribute("aria-expanded", "false", { timeout: 1000 })
  }).toPass()
}

export async function settleAnimations(page: Page): Promise<void> {
  await page.evaluate(async () => {
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
    const animations = document
      .getAnimations()
      .filter((animation) => animation.effect?.getTiming().iterations !== Infinity)
    await Promise.all(animations.map((animation) => animation.finished.catch(() => {})))
  })
}
