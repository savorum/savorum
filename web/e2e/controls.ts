import { expect, type Locator, type Page } from "@playwright/test"

/**
 * Waits until choosing has finished moving focus.
 *
 * A chooser hands focus back to its trigger as it closes, a frame or two after
 * the list goes. A test that types into the next field before that happens
 * loses the caret to the trigger part way through a word, so this waits for
 * the trigger to hold focus — or to have left the page, where choosing
 * navigated.
 */
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

/**
 * Opens a chooser and returns its own list.
 *
 * Every list is drawn in the same portal, and a screen can offer the same
 * option twice ("Applies to temperature 1" and "Applies to temperature 2"
 * both offer "other"), so the list is found through the trigger that controls
 * it rather than by searching the page.
 */
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

/**
 * Chooses an item from a row's action menu.
 *
 * A menu that had to scroll into view can still be settling into place, so a
 * click at the item's previous position selects nothing and the menu stays
 * open. The click is repeated only in that case.
 */
export async function chooseAction(page: Page, menu: string, item: string): Promise<void> {
  const trigger = page.getByRole("button", { name: menu, exact: true })
  await trigger.click()
  await expect(async () => {
    const choice = page.getByRole("menuitem", { name: item, exact: true })
    if (await choice.isVisible()) await choice.click()
    await expect(trigger).toHaveAttribute("aria-expanded", "false", { timeout: 1000 })
  }).toPass()
}
