# Accessibility release checklist

The maintainer runs this manual pass before every release. Savorum targets
WCAG 2.2 AA for the interface it owns, and automated checks only catch
structural regressions.

Use a production build with representative recipes and books, and VoiceOver
with Safari on macOS or NVDA with Firefox or Chrome on Windows. Don't substitute
simulated results for screen-reader testing.

## Keyboard

- Start at the address bar, press Tab, and use **Skip to main content**.
- Reach every primary action without a pointer, with focus always visible.
- Search, filter, open a recipe, edit it, and save it using only the keyboard.
- Reorder and remove an ingredient and a method step. Focus should follow the
  moved item or land on a nearby surviving item, and the change should be
  announced.
- Open and dismiss menus, choosers, popovers, and confirmation dialogs with
  Enter, Space, Escape, and arrow keys. Focus returns to the opening control. In
  a chooser, also check type-ahead, Home and End, and the current option's tick.
- Exercise books, import, print/export, trash, and restore controls.
- Press Ctrl+Enter / Command+Enter in an ingredient and a step, including during
  active composition, where it must not insert a row.
- Check group/stage movement, photo ordering, and removal of the final section.
- Repeat multi-character entry and composition in sign-in, filters, source
  capture, merging, plain-text import, and serving-size entry.

## Screen reader

- Check one library page, recipe reader, recipe editor, book, and dialog.
- Confirm one main content region, a coherent heading hierarchy, named form
  fields, announced validation errors, button state, and status updates.
- Confirm each control in a repeated list announces its own label and position.
- Confirm ingredients and method steps read in authored order, and that images
  have useful alternative text or are silent when decorative.

## Visual behavior

- At 200% zoom and a narrow viewport, core content and controls reflow without
  horizontal loss or overlap. With text spacing overrides too, open the
  optional editor panels.
- With reduced motion on, route changes, dialogs, menus, tooltips, and the
  import progress indicator stay usable without meaningful movement. A still
  indeterminate indicator must read as work in progress, not a finished or
  empty bar.
- Progress indicators announce their phase. Work with a measurable end reports
  a value; work with an unknowable end omits it rather than inventing one.
- In normal and high-contrast settings, no focus ring, status, or control state
  may depend on color alone. Alerts show status as a shape as well as a tint;
  read one of each kind with color removed.
- Test forced colors for control boundaries, selected states, and focus rings.

## Record the result

Record the browser and version, assistive technology, automated matrix run
(against the production binary in CI), and any exception in the release notes.
A failed item blocks the release unless the release
notes document the limitation and it is tracked for a fix.

## Surfaces and extra manual checks

| Surface | Additional manual check |
| --- | --- |
| Library, search, filters | Search and filter with keyboard; text spacing |
| Recipe reader and editor | Read order, contrast, optional details |
| Books | Chapter traversal and membership |
| Import | Review, results and errors |
| Account, preferences, tags, saved searches | Validation and disclosure traversal |
| Cooking | Read remaining time and complete steps |
| History and trash | Restore and error focus |
| Print/export | Preview navigation, export options, rendered document |
