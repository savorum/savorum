/**
 * Whether a single-key page shortcut must yield: the key is already handled,
 * typed with a modifier, or meant for a field, dialog, or open menu.
 */
export function keyTaken(event: KeyboardEvent): boolean {
  if (event.defaultPrevented || event.repeat || event.isComposing || event.metaKey) return true
  // Some layouts type brackets with AltGr, which also reports Control.
  if ((event.ctrlKey || event.altKey) && !event.getModifierState("AltGraph")) return true
  if (
    document.querySelector(
      '[role="dialog"]:not([data-state="closed"]):not([hidden]), [role="alertdialog"]:not([data-state="closed"]):not([hidden]), [role="menu"][data-state="open"]',
    )
  )
    return true
  const target = event.target instanceof Element ? event.target : undefined
  if (!target) return false
  if (target instanceof HTMLElement && target.isContentEditable) return true
  return Boolean(
    target.closest(
      "input, textarea, select, [role=dialog], [role=alertdialog], [role=menu], [role=listbox], [role=combobox], [role=spinbutton], [role=slider], [role=tree], [role=grid], [role=tablist], [role=radiogroup], audio, video",
    ),
  )
}
