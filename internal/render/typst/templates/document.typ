#import "theme.typ": *
#import "card.typ": card-bodies
#import "page.typ": recipe-body, recipe-page-state, recipe-title-state
#import "sheet.typ": impose, length-of

// Everything here is arrangement; the content was decided before. Recipe
// content arrives as structured input, never as source, so it cannot be read
// as markup or code, or name a file, address, or package.

#let doc = sys.inputs
#let options = doc.options
#let format = doc.format
#let sizes = scale-for(format.size)
#let recipes = doc.at("recipes", default: ())

#let is-card = format.family == "card"
#let card-size = format.at("card", default: none)
#let is-imposed = is-card and card-size != none

#let sheet-width = length-of(format.width)
#let sheet-height = length-of(format.height)
#let card-width = if is-imposed { length-of(card-size.width) } else { sheet-width }
#let card-height = if is-imposed { length-of(card-size.height) } else { sheet-height }

#let language = lang-of(doc.at("language", default: ""))
#let region = region-of(doc.at("language", default: ""))

#set document(
  title: doc.at("title", default: labels.recipes),
  author: "Savorum",
  // No creation date, so output is reproducible.
  date: none,
)

#set text(
  font: serif,
  size: sizes.body,
  fill: ink,
  lang: language,
  region: region,
  dir: direction-of(doc.at("direction", default: "ltr")),
  // A card's narrow column hyphenates rather than overflow.
  hyphenate: is-card,
)
#set par(leading: sizes.leading, justify: false)

#set page(
  width: if is-imposed { sheet-width } else { card-width },
  height: if is-imposed { sheet-height } else { card-height },
  margin: if is-imposed { 0pt } else { sizes.margin },
  header: if is-card { none } else {
    context {
      // A recipe that runs past its first page says which recipe it is, so a
      // loose second page is never anonymous.
      if recipe-title-state.get() != "" and here().page() > recipe-page-state.get() {
        text(font: sans, size: sizes.meta, fill: quiet)[
          #recipe-title-state.get() #h(1fr) #labels.continued
        ]
        v(-0.4em)
        hairline
      }
    }
  },
  footer: if is-card { none } else {
    context {
      let total = counter(page).final().first()
      if total > 1 {
        align(center, text(font: sans, size: sizes.meta, fill: quiet)[
          #local-number(counter(page).get().first()) / #local-number(total)
        ])
      }
    }
  },
)

// cards-for lays one recipe out at the exact card size. An automatic image is
// kept only when it does not cost the cook another card of working
// information.
#let cards-for(printed, inner-width, inner-height) = {
  let bodies = card-bodies(printed, sizes, options, inner-width, inner-height)
  if options.image == "automatic" and printed.at("image", default: none) != none {
    let plain = card-bodies(
      printed, sizes, (..options, image: "omit"), inner-width, inner-height,
    )
    if bodies.len() > plain.len() { bodies = plain }
  }
  bodies
}

#context {
  if is-card {
    let inner-width = card-width - 2 * sizes.margin
    let inner-height = card-height - 2 * sizes.margin
    let cards = ()
    for printed in recipes {
      for body in cards-for(printed, inner-width, inner-height).map(body => in-recipe-text(printed, body)) {
        cards.push(if is-imposed {
          block(
            width: card-width,
            height: card-height,
            stroke: 0.3pt + rule-colour,
            inset: sizes.margin,
            body,
          )
        } else {
          body
        })
      }
    }
    if is-imposed {
      for (index, sheet) in impose(cards, doc.sheet, card-width, card-height).enumerate() {
        if index > 0 { pagebreak() }
        sheet
      }
    } else {
      for (index, card) in cards.enumerate() {
        if index > 0 { pagebreak() }
        card
      }
    }
  } else {
    let body-width = sheet-width - 2 * sizes.margin
    let available = sheet-height - 2 * sizes.margin
    for (index, printed) in recipes.enumerate() {
      if index > 0 {
        // Clear the running title so the next recipe is not marked continued.
        recipe-title-state.update("")
        pagebreak()
      }
      in-recipe-text(printed, recipe-body(printed, sizes, options, body-width, available))
    }
  }
}
