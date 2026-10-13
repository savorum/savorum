#import "theme.typ": *

// A full page's fixed hierarchy: what the recipe is, what it takes to plan,
// what to get, what to do, and what is worth knowing afterwards. Ingredients
// and method are set differently so the eye lands in the right one.

#let recipe-title-state = state("savorum-recipe-title", "")
#let recipe-page-state = state("savorum-recipe-page", 1)

#let ingredients-column(printed, sizes, width) = {
  let groups = printed.at("ingredient_groups", default: ())
  let equipment = printed.at("equipment", default: ())
  if not groups.any(g => g.at("ingredients", default: ()).len() > 0) and equipment.len() == 0 { return none }
  let ingredient-count = groups.fold(
    0, (total, group) => total + group.at("ingredients", default: ()).len(),
  )
  // Ordinary lists can afford a little more separation. Dense baking lists
  // keep the compact baseline so whitespace alone never adds a page.
  let row-gap = if ingredient-count <= 16 { ingredient-gap + 0.14em } else { ingredient-gap }
  // A prose qualifier such as "as a garnish" should not make every ingredient
  // name pay for an unusually wide amount. Let that rare amount wrap in its
  // own end-aligned lane and preserve the more valuable name measure.
  let amount-width = amount-column-width(sizes, printed, width * 0.34)
  let kept = printed.at("kept_as_written", default: "")
  block(width: 100%)[
    #column-title(sizes, labels.ingredients)
    #if kept != "" {
      block(width: 100%, inset: (bottom: 0.3em), text(
        font: sans, size: sizes.meta, fill: quiet,
      )[#kept])
    }
    #for group in groups {
      let rows = group.at("ingredients", default: ())
      if rows.len() > 0 {
        let title = group.at("title", default: "")
        if title != "" { group-title(sizes, title) }
        for row in rows {
          ingredient-row(sizes, row, amount-width)
          v(row-gap, weak: true)
        }
      }
    }
    #if equipment.len() > 0 {
      v(0.7em, weak: true)
      column-title(sizes, labels.equipment)
      for row in equipment {
        ingredient-row(sizes, row, amount-width)
        v(row-gap, weak: true)
      }
    }
  ]
}

#let method-column(printed, sizes) = {
  let groups = printed.at("step_groups", default: ())
  if not groups.any(g => g.at("steps", default: ()).len() > 0) { return none }
  block(width: 100%)[
    #column-title(sizes, labels.method)
    #for group in groups {
      let steps = group.at("steps", default: ())
      if steps.len() > 0 {
        let title = group.at("title", default: "")
        if title != "" { group-title(sizes, title) }
        for step in steps {
          step-row(sizes, step)
          v(step-gap, weak: true)
        }
      }
    }
  ]
}

#let closing-block(printed, sizes, options) = {
  let notes = printed.at("notes", default: ())
  let credit = attribution-line(sizes, printed)
  let shows-notes = notes.len() > 0 and options.content.notes
  let shows-credit = credit != none and options.content.source
  let facts = facts-block(sizes, printed)
  if not shows-notes and not shows-credit and facts == none { return none }
  block(width: 100%, breakable: true)[
    #v(0.6em, weak: true)
    #if facts != none {
      facts
      v(0.4em, weak: true)
    }
    #if shows-notes {
      column-title(sizes, labels.notes)
      for note in notes {
        block(width: 100%)[
          #set par(leading: note-leading)
          #text(font: serif, size: sizes.body, fill: ink)[#note]
        ]
        v(note-gap, weak: true)
      }
    }
    // The source credit is a colophon, set off by a rule.
    #if shows-credit [
      #block(width: 100%, inset: (top: 0.9em))[
        #hairline
        #v(0.5em, weak: true)
        #credit
      ]
    ]
  ]
}

#let masthead(printed, sizes, options, width, with-image) = {
  let picture = printed.at("image", default: none)
  let shows-image = with-image and picture != none and options.image != "omit"
  let picture-fraction = sizes.at("image-width", default: 0.32)
  let words = block(width: 100%)[
    #block(width: 100%, inset: (bottom: 0.45em))[
      #set par(leading: title-leading)
      #set text(font: serif, size: sizes.title, weight: "semibold", fill: ink)
      #printed.title
    ]
    #if printed.at("description", default: "") != "" and options.content.description [
      // An inset rather than trailing spacing: weak space at the end of a
      // block is dropped, and a headnote needs air beneath it whether or not
      // anything follows closely.
      #block(width: 100%, inset: (bottom: 0.5em))[
        #text(font: serif, size: sizes.subtitle, style: "italic", fill: quiet)[
          #printed.description
        ]
      ]
    ]
    #let meta = meta-line(sizes, printed)
    #if meta != none [
      #meta
    ]
  ]
  block(width: 100%, breakable: false)[
    #if shows-image {
      let picture-width = width * picture-fraction
      grid(
        columns: (1fr, picture-width),
        column-gutter: width * 0.04,
        words,
        {
          image(picture.name, width: 100%)
          let credit = image-credit(sizes, picture)
          if credit != none { block(width: 100%, inset: (top: 0.25em), credit) }
        },
      )
    } else {
      words
    }
    #v(0.82em, weak: true)
    #hairline
    #v(0.72em, weak: true)
  ]
}

#let stacked-body(printed, sizes, options, width) = {
  let ingredients = ingredients-column(printed, sizes, width)
  let method = method-column(printed, sizes)
  block(width: 100%)[
    #if ingredients != none [#ingredients #v(1em, weak: true)]
    #if method != none [#method]
    #closing-block(printed, sizes, options)
  ]
}

#let side-by-side-body(printed, sizes, options, width) = {
  // Unequal columns: a little over a third for ingredients, the rest for the
  // method's prose.
  let left = width * 0.37
  let gutter = width * 0.045
  let ingredients = ingredients-column(printed, sizes, left)
  let method = method-column(printed, sizes)
  if ingredients == none or method == none {
    return stacked-body(printed, sizes, options, width)
  }
  block(width: 100%)[
    #grid(
      columns: (left, 1fr),
      column-gutter: gutter,
      ingredients,
      block(width: 100%)[
        #method
        #closing-block(printed, sizes, options)
      ],
    )
  ]
}

#let height-of(body, width) = if body == none { 0pt } else {
  measure(block(width: width, body)).height
}

// choose-layout asks the typesetter what each composition would actually cost
// rather than counting ingredients. A recipe with long ingredient names grows
// tall in a narrow column, and the measurement says so.
#let choose-layout(printed, sizes, options, width, available) = {
  if options.layout != "automatic" { return options.layout }
  let ingredients = printed.at("ingredient_groups", default: ())
  let count = ingredients.fold(0, (total, group) => total + group.at("ingredients", default: ()).len())
  if count < 4 { return "stacked" }
  let left = width * 0.37
  let narrow = height-of(ingredients-column(printed, sizes, left), left)
  let wide = height-of(ingredients-column(printed, sizes, width), width)
  let method = height-of(method-column(printed, sizes), width * 0.585)
  let stacked-height = wide + height-of(method-column(printed, sizes), width)
  let side-height = calc.max(narrow, method)
  // A narrow column that has to run past the page it starts on, or that costs
  // more room than setting the recipe in one column, is the wrong shape.
  if narrow > available { return "stacked" }
  if side-height >= stacked-height { return "stacked" }
  "side-by-side"
}

// The paragraph leading applies to what is measured as well as what is drawn,
// so measured heights match the printed page.
#let leaded(sizes, body) = {
  set par(leading: sizes.leading)
  body
}

#let compose(printed, sizes, options, width, available, with-image) = {
  let layout = choose-layout(printed, sizes, options, width, available)
  let head = leaded(sizes, masthead(printed, sizes, options, width, with-image))
  let body = leaded(sizes, if layout == "side-by-side" {
    side-by-side-body(printed, sizes, options, width)
  } else {
    stacked-body(printed, sizes, options, width)
  })
  (head: head, body: body, height: height-of(head, width) + height-of(body, width))
}

// Enlargements tried for a short recipe, largest first; the first that still
// fits on one page wins, and otherwise the baseline is used. Nothing is set
// smaller than the baseline.
#let expansions = (1.10, 1.05)

// The title grows more slowly than the text.
#let expanded(sizes, factor) = (
  margin: sizes.margin,
  title: sizes.title * calc.min(factor, 1.14),
  subtitle: sizes.subtitle * calc.min(factor, 1.18),
  column-title: sizes.column-title * factor,
  group: sizes.group * factor,
  body: sizes.body * factor,
  amount: sizes.amount * factor,
  meta: sizes.meta * factor,
  // Slightly looser than proportional. Leading is already relative to the type
  // size, so this is the extra air a page with room to spare can afford.
  leading: sizes.leading * 1.03,
  // A photograph grows with the page it sits on, up to the point where it
  // would start crowding the words beside it.
  image-width: calc.min(sizes.at("image-width", default: 0.32) * factor, 0.42),
)

// pages-needed reports how many pages a composition would occupy, which is how
// an automatic image decides whether it costs the reader an extra page.
#let pages-needed(height, available) = {
  if available <= 0pt { return 1 }
  calc.max(1, calc.ceil(height / available))
}

// likely-short estimates, from the amount of text alone, whether a recipe
// fits on one page at its largest enlargement. It only decides which
// composition is measured first, never which one is chosen.
#let likely-short(printed) = {
  let volume = printed.at("description", default: "").len()
  for group in printed.at("ingredient_groups", default: ()) {
    for ingredient in group.at("ingredients", default: ()) {
      volume += 60 + ingredient.at("text", default: "").len()
    }
  }
  for group in printed.at("step_groups", default: ()) {
    for step in group.at("steps", default: ()) {
      volume += 60 + step.at("text", default: "").len()
    }
  }
  for note in printed.at("notes", default: ()) {
    volume += 60 + note.len()
  }
  volume <= 3000
}

#let recipe-body(printed, sizes, options, width, available) = {
  let picture = printed.at("image", default: none)
  let wants-image = picture != none and options.image != "omit"
  // The margin allows for pagination of unbreakable blocks differing from the
  // measured height.
  let fits(composition) = composition.height <= available * 0.96
  let enlarged(factor) = compose(
    printed, expanded(sizes, factor), options, width, available, wants-image,
  )
  // Larger type is never shorter. When the largest enlargement fits, the
  // baseline fits with its photograph and would be enlarged to it, so the
  // measurements that decide those are skipped. Every measured layout stays
  // in memory until the document is finished.
  let largest = if likely-short(printed) { enlarged(expansions.first()) }
  let settled = largest != none and fits(largest)
  let composed = if settled { largest } else {
    compose(printed, sizes, options, width, available, wants-image)
  }
  if not settled and wants-image and options.image == "automatic" {
    let without = compose(printed, sizes, options, width, available, false)
    // Automatic keeps a photograph only when it does not cost the cook a page
    // of working information.
    if pages-needed(composed.height, available) > pages-needed(without.height, available) {
      composed = without
    }
  }
  // A recipe with room to spare is set larger.
  if not settled and fits(composed) {
    for factor in expansions {
      let bigger = if factor == expansions.first() and largest != none { largest } else { enlarged(factor) }
      if fits(bigger) {
        composed = bigger
        break
      }
    }
  }
  recipe-title-state.update(printed.title)
  context recipe-page-state.update(here().page())
  composed.head
  composed.body
}
