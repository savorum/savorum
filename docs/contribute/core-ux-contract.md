# Core recipe UX contract

Read this before you change the web interface. These are the promises the
browser suite protects; if your change touches one, its test changes too. They
put the [product principles](product-principles.md) into testable form and
apply to local mode and shared libraries alike. In a local installation, accounts,
roles, and library selection never appear in the everyday recipe path. With
sign-in enabled, authorization protects the same workflows.

## Finding and reading

- The library opens as a grid of recipe cards with an image or placeholder,
  with search and filters visible. The search box suggests the library's tags,
  categories, and books for what is typed, without replacing text search.
- Filters start with personal shortcuts. Each vocabulary is behind a control
  named for it.
- Opening a recipe shows ingredients and method without another click. On a
  desktop both columns fit the first screen. On a narrow screen they stack
  without horizontal scrolling.
- **Cook** is always a named action. The measurement control names the current
  choice and offers the others on request. The rating appears with cooking
  history after the recipe.
- A book opens as a table of contents. Arranging it is a separate action.
  Adding recipes is a search that can add several at once. Publication details
  are behind a named, summarized section.
- Navigation names places: Recipes and Books. Capture is offered from Recipes
  beside writing a recipe. Imports sits with library maintenance. Secondary
  menus group items by purpose, keeping library maintenance apart from personal
  settings. An action on selected recipes is offered where they're selected.
- A capture is one task. Under review it fills the screen, and the library
  returns when it's saved or cancelled. Browser capture tools are offered after
  a first capture, never before pasting a link.

## Consistent controls and wording

- Every list of options uses the same drawn chooser, with group names and a
  tick for the current option.
- Every message beside the work (refusals, warnings, confirmations) uses the
  shared panel, whose tone shows as an icon shape as well as color.
- Screens use cooking language. Revisions, facet terms, portable IDs, and raw
  language tags aren't everyday controls. Recipe parts are named for what they
  are, never by stored field name, on screen and in accessible names. Format
  names appear only in export menus, and revision details only in history.
  Recipe text itself is never changed to meet this rule.

## Editing

- The editor follows the order a recipe is written: title and summary, yield
  and time, ingredients, instructions, then notes and variations.
- A simple yield needs no disclosure, and changing an ingredient needs no
  decision about how its amount is stored. Uncommon metadata is behind named
  disclosures, and empty optional fields don't fill the screen.
- A recipe's own labels and the library's filing stay separate and survive
  unrelated edits. Dates are entered as dates.
- Personal display defaults live in preferences. The editor's local selector
  says it's local and links there.
- Typing several characters, inserting at the caret, and text composition keep
  the same focused element and the complete value. Adding, moving, removing,
  and saving rows keep their meaning and order.

## Using a recipe

- Scaling and measurement choices change what is displayed, never what is
  stored. An adjustment equal to the recipe's own value isn't shown. A shown
  adjustment says which reading is in force and names the alternative by value.
- When a batch can't recalculate every amount, the unchanged rows are marked in
  the recipe, in cooking, and in print, and the batch control counts them. A
  printed batch includes the same notice.
- A row's accessible name is the amount shown. The source wording is exposed as
  identified provenance.
- Cooking shows one step at a time, with the ingredients beside it and the
  recipe page's timings, including adjustments. **Next step** marks the step
  done and advances. Going back marks the previous step not done again. The
  whole method stays reachable with the recipe's own section numbering.
- Check-offs survive reload and exit. **Resume cooking** continues and
  **Start over** clears check-offs. **Finish cooking** records a cooking event
  without a new revision. **Discard cooking** ends the session without
  recording it. Batch, measurements, screen wakefulness, and **Start over** are
  under **Cooking options**.
- Timers are real, persistent countdowns that link back to their recipe.
- Print and export produce the promised files.

## Organizing

- Before a tag, category, or book is renamed, combined, or deleted, the screen
  shows how many recipes use it. Combining is its own task and names the label
  that remains and the recipes that move. Deleting one that recipes carry
  asks first and says how many recipes it comes off.
- Filing one recipe shows what it already carries and offers the rest of each
  vocabulary behind a named control.
- A bulk edit of selected recipes is described in sentences naming each label
  change and the recipes affected, readable without operating a control. Labels
  it doesn't name stay unchanged, and cancelling changes nothing.
- Comparing starts from two selected recipes, marks differences, shows matching
  parts once, and returns to the same selection. Combining is a separate step:
  it names the recipe kept and the one trashed, shows which part comes from
  which, previews the result, and states that only whole parts can be taken.
- Destructive actions move items to the trash, where they can be restored.
  Permanent deletion needs a separate confirmation.
- An import preview gives enough information to decide. A single captured
  recipe shows as that recipe with a way to save it, without queue controls. A
  possible duplicate still waits for a decision.

## Tests

`just check` runs the Go application with its embedded frontend through
Playwright, and CI runs the same suite. Browser tests live in `web/e2e`;
`core-contract.spec.ts` is the main suite. A change to a promised control
updates its test, and new workflows include keyboard use and error recovery.
Manual checks automation can't cover are in the
[accessibility checklist](accessibility-checklist.md).
