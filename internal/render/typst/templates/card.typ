#import "theme.typ": *

// A recipe that needs more room than one card continues onto another card at
// the same size. Groups are kept together when they can be, a heading is never
// left alone at the foot of a card, and a step is divided only when it is
// larger than a card.

#let continuation-mark = sym.arrow.r

#let make-atom(
  body, group: "", keep: false, cohesive: false, words: (), build: none, rest-build: none,
  gap: none,
) = (
  body: body,
  group: group,
  // The space that follows this piece, which must exceed its internal leading
  // (see theme.typ). `none` takes the card's default.
  gap: gap,
  // keep marks a heading, which must be followed on the same card by the first
  // thing it introduces.
  keep: keep,
  // cohesive marks a group worth keeping whole, such as a list of measures but
  // not a run of steps.
  cohesive: cohesive,
  words: words,
  build: build,
  rest-build: if rest-build == none { build } else { rest-build },
)

// atoms-for builds the ordered pieces a card is packed from. Each piece
// carries what the packer needs to know about it: which group it belongs to,
// whether it must be followed by something, and how to divide it if it turns
// out to be larger than a whole card.
#let atoms-for(printed, sizes, options, amount-width, ingredient-columns, card-height) = {
  let atoms = ()

  let description = printed.at("description", default: "")
  if description != "" and options.content.description {
    let build(part, more) = text(
      font: serif, size: sizes.meta, style: "italic", fill: quiet,
    )[#part.join(" ")#if more [ #continuation-mark]]
    atoms.push(make-atom(
      build(description.split(" "), false),
      group: "headnote",
      words: description.split(" "),
      build: build,
      gap: sizes.meta * 0.7,
    ))
  }

  let meta = meta-line(sizes, printed)
  if meta != none {
    atoms.push(make-atom(block(width: 100%, inset: (top: 0.25em), meta), group: "meta"))
  }

  let picture = printed.at("image", default: none)
  if picture != none and options.image != "omit" {
    // A card's photograph is a cropped band: the card's width and a fixed
    // share of its height.
    atoms.push(make-atom(
      block(width: 100%, inset: (y: 0.2em), box(
        width: 100%,
        height: card-height * 0.24,
        clip: true,
        image(picture.name, width: 100%, height: 100%, fit: "cover"),
      )),
      group: "image",
    ))
    let credit = image-credit(sizes, picture)
    if credit != none { atoms.push(make-atom(block(width: 100%, credit), group: "image-credit")) }
  }

  let ingredient-groups = printed.at("ingredient_groups", default: ())
  if ingredient-groups.any(g => g.at("ingredients", default: ()).len() > 0) {
    atoms.push(make-atom(
      column-title(sizes, labels.ingredients), group: "ingredients-title", keep: true,
    ))
    // The batch's own caveat belongs with the list it is about, so a card
    // carrying measures never carries them without it.
    let kept = printed.at("kept_as_written", default: "")
    if kept != "" {
      atoms.push(make-atom(
        block(width: 100%, text(font: sans, size: sizes.meta, fill: quiet)[#kept]),
        group: "ingredients-title",
        keep: true,
      ))
    }
    for (index, group) in ingredient-groups.enumerate() {
      let rows = group.at("ingredients", default: ())
      if rows.len() == 0 { continue }
      let title = group.at("title", default: "")
      let name = "ingredients-" + str(index)
      if title != "" {
        atoms.push(make-atom(
          group-title(sizes, title), group: name, keep: true, cohesive: true,
        ))
      }
      if ingredient-columns == 2 {
        let row-index = 0
        while row-index < rows.len() {
          let left = ingredient-row(sizes, rows.at(row-index), amount-width)
          let right = if row-index + 1 < rows.len() {
            ingredient-row(sizes, rows.at(row-index + 1), amount-width)
          } else { none }
          atoms.push(make-atom(
            block(width: 100%)[
              #grid(columns: (1fr, 1fr), column-gutter: 0.85em, left, right)
            ],
            group: name,
            cohesive: true,
          ))
          row-index += 2
        }
      } else {
        for row in rows {
          atoms.push(make-atom(
            ingredient-row(sizes, row, amount-width), group: name, cohesive: true,
          ))
        }
      }
    }
  }

  let step-groups = printed.at("step_groups", default: ())
  if step-groups.any(g => g.at("steps", default: ()).len() > 0) {
    atoms.push(make-atom(column-title(sizes, labels.method), group: "method-title", keep: true))
    for (index, group) in step-groups.enumerate() {
      let steps = group.at("steps", default: ())
      if steps.len() == 0 { continue }
      let title = group.at("title", default: "")
      let name = "method-" + str(index)
      if title != "" {
        atoms.push(make-atom(group-title(sizes, title), group: name, keep: true))
      }
      for step in steps {
        let words = step.text.split(" ")
        let fragment(part, more, numbered) = step-row(
          sizes,
          (
            number: step.number,
            number_text: step.at("number_text", default: str(step.number)),
            title: if numbered { step.at("title", default: "") } else { "" },
            text: part.join(" ") + if more { " " + continuation-mark } else { "" },
            phases: if more { () } else { step.at("phases", default: ()) },
          ),
          continued: not numbered,
        )
        let build(part, more) = fragment(part, more, true)
        let rest(part, more) = fragment(part, more, false)
        atoms.push(make-atom(
          build(words, false), group: name, words: words, build: build, rest-build: rest,
          gap: sizes.body * 0.8,
        ))
      }
    }
  }

  let notes = printed.at("notes", default: ())
  if notes.len() > 0 and options.content.notes {
    atoms.push(make-atom(column-title(sizes, labels.notes), group: "notes-title", keep: true))
    for note in notes {
      let words = note.split(" ")
      let build(part, more) = block(width: 100%)[
        #set par(leading: note-leading)
        #text(font: serif, size: sizes.body, fill: ink)[
          #part.join(" ")#if more [ #continuation-mark]
        ]
      ]
      atoms.push(make-atom(
        build(words, false), group: "notes", words: words, build: build,
        gap: sizes.body * 0.75,
      ))
    }
  }

  let facts = facts-block(sizes, printed)
  if facts != none {
    atoms.push(make-atom(block(width: 100%, inset: (top: 0.4em), facts), group: "facts"))
  }

  let credit = attribution-line(sizes, printed)
  if credit != none and options.content.source {
    atoms.push(make-atom(block(width: 100%, inset: (top: 0.4em), credit), group: "source"))
  }
  atoms
}

// height-of measures one piece at the width it will be set at.
#let height-of(body, width) = measure(block(width: width, body)).height

// fit-words finds the largest number of leading words that still fit, which is
// how a step larger than a whole card is divided without losing any of it.
#let fit-words(build, words, width, available) = {
  let low = 1
  let high = words.len()
  let best = 0
  while low <= high {
    let middle = int((low + high) / 2)
    if height-of(build(words.slice(0, middle), true), width) <= available {
      best = middle
      low = middle + 1
    } else {
      high = middle - 1
    }
  }
  best
}

// pack distributes the pieces over as many cards as the recipe needs.
#let pack(source, width, first-height, later-height, spacing) = {
  let gap-of(atom) = if atom.at("gap", default: none) == none { spacing } else { atom.gap }
  let items = source
  let heights = items.map(a => height-of(a.body, width) + gap-of(a))
  let group-heights = (:)
  let group-start = (:)
  let group-end = (:)
  for (index, atom) in items.enumerate() {
    group-heights.insert(atom.group, group-heights.at(atom.group, default: 0pt) + heights.at(index))
    if atom.group not in group-start { group-start.insert(atom.group, index) }
    group-end.insert(atom.group, index)
  }

  let cards = ()
  let current = ()
  let used = 0pt
  let index = 0
  let guard = 0
  while index < items.len() and guard < 5000 {
    guard += 1
    let atom = items.at(index)
    let capacity = if cards.len() == 0 { first-height } else { later-height }
    let remaining = capacity - used
    let height = heights.at(index)

    // A group worth keeping whole is not split across the boundary just
    // because this card happens to be nearly full, provided it is small enough
    // that moving it would not strand most of a card.
    let would-split-avoidably = (
      atom.cohesive
        and group-start.at(atom.group) == index
        and current.len() > 0
        and group-heights.at(atom.group) > remaining
        and group-heights.at(atom.group) <= later-height * 0.7
    )

    // A heading needs the first thing it introduces beside it.
    let successor-fits = (
      not atom.keep
        or index + 1 >= items.len()
        or heights.at(index + 1) <= remaining - height
    )

    // A single trailing row left alone on the next card reads as a mistake, so
    // the break happens one row earlier and the two travel together.
    let leaves-lonely-row = (
      group-end.at(atom.group) == index + 1
        and current.len() > 0
        and height <= remaining
        and heights.at(index + 1) > remaining - height
    )

    // A piece larger than an empty card has to be divided wherever the break
    // falls, so it is better to begin it in the space already on this card
    // than to leave that space blank and divide it on the next one anyway.
    let divisible = atom.build != none and atom.words.len() > 1
    let must-divide = divisible and height > later-height
    let worth-starting = remaining >= later-height * 0.2

    if height <= remaining and successor-fits and not would-split-avoidably and not leaves-lonely-row {
      current.push((atom: atom, part: none))
      used += height
      index += 1
    } else if must-divide and (current.len() == 0 or worth-starting) {
      let taken = fit-words(atom.build, atom.words, width, remaining - gap-of(atom))
      if taken < 1 { taken = 1 }
      if taken >= atom.words.len() {
        // The whole of it turned out to fit after all, so it is set whole
        // rather than divided into a piece and nothing.
        current.push((atom: atom, part: none))
        used += height
        index += 1
      } else {
        current.push((atom: atom, part: taken))
        cards.push(current)
        current = ()
        used = 0pt
        let remainder = atom.words.slice(taken)
        items.at(index).words = remainder
        items.at(index).build = atom.rest-build
        items.at(index).body = (atom.rest-build)(remainder, false)
        heights.at(index) = height-of(items.at(index).body, width) + gap-of(items.at(index))
      }
    } else if current.len() > 0 {
      cards.push(current)
      current = ()
      used = 0pt
    } else if divisible {
      // Larger than an empty card, so divide it.
      let taken = fit-words(atom.build, atom.words, width, capacity - gap-of(atom))
      if taken < 1 { taken = 1 }
      if taken >= atom.words.len() {
        current.push((atom: atom, part: none))
        used += height
        index += 1
      } else {
        current.push((atom: atom, part: taken))
        cards.push(current)
        current = ()
        used = 0pt
        let remainder = atom.words.slice(taken)
        items.at(index).words = remainder
        items.at(index).build = atom.rest-build
        items.at(index).body = (atom.rest-build)(remainder, false)
        heights.at(index) = height-of(items.at(index).body, width) + gap-of(items.at(index))
      }
    } else {
      // Nothing can divide this piece, so it is set whole rather than dropped.
      current.push((atom: atom, part: none))
      used += height
      index += 1
    }
  }
  if current.len() > 0 { cards.push(current) }
  if cards.len() == 0 { cards = ((),) }
  cards
}

// The space below the title is an inset: trailing weak spacing at the end of a
// block is dropped, and a title needs room under it whether or not anything
// happens to follow closely.
#let card-header(printed, sizes, first) = {
  if first {
    block(width: 100%, breakable: false, inset: (bottom: 0.12em))[
      #set par(leading: title-leading)
      #set text(font: serif, size: sizes.title, weight: "semibold", fill: ink)
      #printed.title
    ]
  } else {
    block(width: 100%, breakable: false, inset: (bottom: 0.3em))[
      #text(font: sans, size: sizes.column-title, weight: "semibold", fill: quiet)[#printed.title]
    ]
  }
}

#let card-number(sizes, index, total) = text(
  font: sans, size: sizes.meta, fill: quiet,
)[#local-number(index + 1) / #local-number(total)]

// card-bodies lays one recipe out as the sequence of cards it needs. The
// result is ready to be placed, either as pages of its own size or onto a
// sheet for trimming.
#let card-bodies(printed, sizes, options, width, height) = {
  // The default gap between ingredients; steps and notes ask for their own.
  let spacing = sizes.body * 0.38
  let ingredient-count = printed.at("ingredient_groups", default: ()).fold(
    0, (total, group) => total + group.at("ingredients", default: ()).len(),
  )
  // Wide cards pair a long ingredient list into two columns.
  let ingredient-columns = if width >= 5in and ingredient-count >= 6 { 2 } else { 1 }
  let amount-limit = if ingredient-columns == 2 { width * 0.16 } else { width * 0.34 }
  let amount-width = amount-column-width(sizes, printed, amount-limit)
  let atoms = atoms-for(printed, sizes, options, amount-width, ingredient-columns, height)
  let first-header = height-of(card-header(printed, sizes, true), width)
  let later-header = height-of(card-header(printed, sizes, false), width)
  let cards = pack(atoms, width, height - first-header, height - later-header, spacing)
  let total = cards.len()
  cards
    .enumerate()
    .map(((index, entries)) => block(width: width, height: height)[
      #place(top + end, card-number(sizes, index, total))
      #card-header(printed, sizes, index == 0)
      #for entry in entries {
        let body = if entry.part == none {
          entry.atom.body
        } else {
          (entry.atom.build)(entry.atom.words.slice(0, entry.part), true)
        }
        block(width: 100%, body)
        v(
          if entry.atom.at("gap", default: none) == none { spacing } else { entry.atom.gap },
          weak: true,
        )
      }
    ])
}
