#let labels = sys.inputs.labels

// A BCP 47 tag's primary language, which is what Typst's lang accepts.
#let lang-of(tag, fallback: "en") = {
  let primary = tag.split("-").at(0)
  if primary.len() >= 2 and primary.len() <= 3 { lower(primary) } else { fallback }
}

// A tag's region, which selects regional glyph forms such as Traditional
// Chinese in the CJK fallback. A Han script subtag implies its usual region.
#let region-of(tag) = {
  let parts = tag.split("-").slice(1)
  let singleton = parts.position(part => part.len() == 1)
  if singleton != none { parts = parts.slice(0, singleton) }
  let region = parts.find(part => part.len() == 2)
  if region != none { upper(region) }
  else if parts.any(part => lower(part) == "hant") { "TW" }
  else { none }
}

#let direction-of(value) = if value == "rtl" { rtl } else { ltr }

#let local-number(value) = str(value).replace(
  regex("[0-9]"),
  matched => sys.inputs.at("digits", default: "0123456789").codepoints().at(int(matched.text)),
)

// Recipe content is set in the recipe's own language and direction, which
// may differ from the labels around it.
#let in-recipe-text(printed, body) = {
  let tag = printed.at("language", default: "")
  set text(
    lang: lang-of(tag, fallback: text.lang),
    region: if tag == "" { text.region } else { region-of(tag) },
    dir: direction-of(printed.at("direction", default: "ltr")),
  )
  body
}
// Typography for print. These sizes are chosen for paper, not for a screen:
// full-page body text sits where a book sets it, and card text never falls
// below a size that stays readable on a kitchen counter. When a card runs out
// of room the recipe continues onto another card; the type does not shrink.

#let ink = rgb("#1c1a17")
#let quiet = rgb("#69635c")
#let rule-colour = rgb("#d8d2c9")
#let accent = rgb("#73624d")

// Source faces keep the editorial voice for Latin text. Noto faces are the
// embedded fallbacks for Arabic, Hebrew, Thai, CJK, emoji, and symbols: recipes may name any
// language and stored text must print even when this editor has no
// language-specific layout or parsing support.
#let fallbacks = ("Noto Naskh Arabic", "Noto Sans Hebrew", "Noto Sans Thai", "Noto Sans CJK JP", "Noto Emoji", "Noto Sans Symbols 2")
#let serif = ("Source Serif 4", ..fallbacks)
#let sans = ("Source Sans 3", ..fallbacks)

// The smallest body size a printed recipe is ever set in. Pagination answers
// an overflowing card; type size does not.
#let minimum-body = 10pt

// Display titles are set tighter than body text.
#let title-leading = 0.45em

#let page-scale = (
  "letter": (
    margin: 0.72in,
    title: 26pt,
    subtitle: 10pt,
    column-title: 11pt,
    group: 10.5pt,
    body: 10.5pt,
    amount: 10pt,
    meta: 9pt,
    leading: 0.7em,
  ),
  "a4": (
    margin: 18mm,
    title: 26pt,
    subtitle: 10pt,
    column-title: 11pt,
    group: 10.5pt,
    body: 10.5pt,
    amount: 10pt,
    meta: 9pt,
    leading: 0.7em,
  ),
  "card-3x5": (
    margin: 0.2in,
    title: 13pt,
    subtitle: 8.5pt,
    column-title: 9pt,
    group: 9.5pt,
    body: minimum-body,
    amount: 9.5pt,
    meta: 8pt,
    leading: 0.5em,
  ),
  "card-4x6": (
    margin: 0.24in,
    title: 15.5pt,
    subtitle: 9pt,
    column-title: 9.5pt,
    group: 10pt,
    body: minimum-body,
    amount: 10pt,
    meta: 8pt,
    leading: 0.55em,
  ),
  "card-5x8": (
    margin: 0.3in,
    title: 19pt,
    subtitle: 10pt,
    column-title: 10.5pt,
    group: 10.5pt,
    body: 10.5pt,
    amount: 10.5pt,
    meta: 8.5pt,
    leading: 0.6em,
  ),
)

#let scale-for(size) = page-scale.at(size, default: page-scale.at("letter"))

// A measure is set with the font's own fraction forms, with its plain text
// beside it.
#let number(segment) = {
  if segment.at("denominator", default: "") != "" {
    if segment.at("whole", default: "") != "" {
      [#segment.whole#h(0.12em)]
    }
    text(features: ("frac",))[#segment.numerator/#segment.denominator]
  } else {
    [#segment.at("text", default: "")]
  }
}

// Isolates text formatted by the application, such as an amount or a time,
// so it keeps its own order inside a recipe written in the other direction.
#let label-direction = if sys.inputs.at("direction", default: "ltr") == "rtl" { rtl } else { ltr }
#let isolated(body) = context {
  if text.dir == label-direction { body } else [#"\u{2068}"#body#"\u{2069}"]
}

#let amount-text(amount) = isolated({
  let segments = amount.at("segments", default: ())
  if segments.len() == 0 {
    [#amount.at("text", default: "")]
  } else {
    if amount.at("approximate", default: false) {
      [#labels.about.trim() #h(0.15em)]
    }
    for segment in segments {
      number(segment)
    }
  }
})

// A heading that would otherwise sit alone at the foot of a page is carried
// forward with the first thing it introduces.
#let heading-block(body) = block(sticky: true, breakable: false, body)

#let hairline = line(length: 100%, stroke: 0.5pt + rule-colour)

// An inset rather than weak spacing, which is dropped at the top of a card or
// column.
#let column-title(sizes, body) = heading-block(block(width: 100%, inset: (top: 0.82em))[
  #text(font: sans, size: sizes.column-title, weight: "semibold", fill: accent,
    lang: lang-of(sys.inputs.at("language", default: "en")),
    region: region-of(sys.inputs.at("language", default: "en")),
  )[#body]
  #v(0.28em, weak: true)
  #hairline
])

#let group-title(sizes, body) = heading-block(block(
  width: 100%, inset: (top: 0.7em, bottom: 0.15em),
)[
  #text(font: sans, size: sizes.group, weight: "semibold", fill: ink)[#body]
])

// amount-column-width sizes the measures column to the measures actually in
// the recipe, up to a limit.
#let amount-column-width(sizes, printed, limit) = {
  let widest = 0pt
  for group in printed.at("ingredient_groups", default: ()) {
    for row in group.at("ingredients", default: ()) {
      let shown = measure(text(
        font: sans, size: sizes.amount, weight: "semibold",
      )[#amount-text(row.amount)]).width
      if shown > widest { widest = shown }
    }
  }
  calc.min(widest, limit)
}

// Inside beats outside: within a listed item (measure, step, note) lines sit
// closer than the space to the next item. Each pair below is an item's
// internal leading and the space after it; the second is always larger.
#let ingredient-leading = 0.34em
#let ingredient-gap = 0.58em
#let step-leading = 0.62em
#let step-gap = 1.05em
#let note-leading = 0.58em
#let note-gap = 0.95em

#let ingredient-row(sizes, ingredient, amount-width) = block(breakable: false, width: 100%)[
  #set par(leading: ingredient-leading)
  #let kept = ingredient.at("as_written", default: "")
  #grid(
    columns: (amount-width, 1fr),
    column-gutter: 0.5em,
    align(end + top, text(
      font: sans,
      size: sizes.amount,
      weight: "semibold",
      fill: ink,
    )[#amount-text(ingredient.amount)]),
    {
      text(font: serif, size: sizes.body, fill: ink)[#ingredient.text]
      // Marks a row the batch could not recalculate.
      if kept != "" {
        text(font: sans, size: sizes.meta, fill: quiet)[ (#kept)]
      }
    },
  )
]

#let step-row(sizes, step, continued: false) = block(breakable: false, width: 100%)[
  #set par(leading: step-leading)
  #grid(
    columns: (1.45em, 1fr),
    column-gutter: 0.45em,
    align(end + top, text(font: sans, size: sizes.amount, weight: "semibold", fill: quiet)[
      #if continued [] else [#(step.at("number_text", default: str(step.number))).]
    ]),
    {
      let title = step.at("title", default: "")
      if title != "" {
        text(font: sans, size: sizes.amount, weight: "semibold", fill: ink)[#title]
        linebreak()
      }
      if step.text != "" {
        text(font: serif, size: sizes.body, fill: ink)[#step.text]
      }
      let phases = step.at("phases", default: ())
      if phases.len() > 0 {
        // Recipe-wide phases start their own line.
        if step.text != "" { linebreak() }
        text(font: sans, size: sizes.meta, fill: quiet)[#phases.map(isolated).join(" · ")]
      }
    },
  )
]

#let meta-line(sizes, printed) = {
  // Only a recipe set against the labels' direction needs its parts isolated.
  let isolated = if printed.at("direction", default: "ltr") == sys.inputs.at("direction", default: "ltr") {
    part => part
  } else {
    part => "\u{2068}" + part + "\u{2069}"
  }
  let parts = ()
  let yield-value = printed.at("yield", default: none)
  if yield-value != none { parts.push(isolated(yield-value.text)) }
  for other in printed.at("other_yields", default: ()) { parts.push(isolated(other.text)) }
  for timing in printed.at("timings", default: ()) {
    parts.push(isolated(timing.label + " " + timing.value))
  }
  parts += printed.at("classifications", default: ()).map(isolated)
  let batch = printed.at("batch", default: "")
  if batch != "" { parts.push(isolated(batch)) }
  let convention = printed.at("convention", default: "")
  if convention != "" { parts.push(isolated(convention)) }
  if parts.len() == 0 { return none }
  text(font: sans, size: sizes.meta, fill: quiet)[#parts.join([ #sym.dot.c ])]
}

#let attribution-line(sizes, printed) = {
  let source = printed.at("attribution", default: none)
  if source == none { return none }
  let parts = ()
  if source.at("name", default: "") != "" { parts.push(source.name) }
  let authors = source.at("authors", default: ())
  if authors.len() > 0 { parts.push(authors.join(", ")) }
  let url = source.at("url", default: "")
  if parts.len() == 0 and url == "" { return none }
  block(width: 100%)[
    #set par(leading: 0.35em)
    #if parts.len() > 0 {
      text(font: sans, size: sizes.meta, fill: quiet)[#parts.join([ #sym.dot.c ])]
    }
    #if url != "" {
      if parts.len() > 0 { linebreak() }
      text(font: sans, size: sizes.meta * 0.9, fill: quiet)[#isolated(url)]
    }
  ]
}

// The source's own dietary and allergen statements, then its nutrition. Both
// are printed as the source wrote them and set quietly after the method.
#let facts-block(sizes, printed) = {
  let claims = printed.at("claims", default: ())
  let nutrition = printed.at("nutrition", default: ())
  if claims.len() == 0 and nutrition.len() == 0 { return none }
  block(width: 100%)[
    #set par(leading: 0.4em)
    #if claims.len() > 0 {
      column-title(sizes, labels.claims)
      text(font: sans, size: sizes.meta, fill: ink)[#claims.join([ #sym.dot.c ])]
      v(0.6em, weak: true)
    }
    #if nutrition.len() > 0 {
      column-title(sizes, labels.nutrition)
      for statement in nutrition {
        let values = statement.at("values", default: ()).map(v => v.label + " " + v.value)
        let basis = statement.basis
        if statement.at("as_written", default: false) { basis += " (" + labels.nutrition_as_written + ")" }
        block(width: 100%)[
          #text(font: sans, size: sizes.meta, weight: "semibold", fill: ink)[#basis]
          #linebreak()
          #text(font: sans, size: sizes.meta, fill: ink)[#values.join([ #sym.dot.c ])]
          #if statement.at("note", default: "") != "" {
            linebreak()
            text(font: sans, size: sizes.meta * 0.9, fill: quiet)[#statement.note]
          }
        ]
        v(0.4em, weak: true)
      }
    }
  ]
}

#let image-credit(sizes, picture) = {
  let credit = picture.at("credit", default: "")
  if credit == "" { return none }
  text(font: sans, size: sizes.meta * 0.85, fill: quiet)[#labels.photo#credit]
}
