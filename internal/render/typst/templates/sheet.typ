#import "theme.typ": *

// Imposition places finished cards, never scaled, on a sheet for printing and
// trimming.

// A length arrives as the magnitude and the unit it was defined in, so a card
// keeps its exact dimensions rather than a rounded conversion of them.
#let length-of(value) = {
  let unit = value.at("unit", default: "pt")
  value.value * (if unit == "in" { 1in } else if unit == "mm" { 1mm } else { 1pt })
}

#let mark-colour = rgb("#9a9a9a")
#let mark-length = 8pt
#let mark-offset = 3pt

// crop-marks draws short rules outside each corner of a card so a trimmed
// edge is obvious without a line printing across the card itself.
#let crop-marks(width, height) = {
  let stroke-style = 0.3pt + mark-colour
  let horizontal(x, y) = place(dx: x, dy: y, line(length: mark-length, stroke: stroke-style))
  let vertical(x, y) = place(dx: x, dy: y, line(
    length: mark-length, angle: 90deg, stroke: stroke-style,
  ))
  horizontal(-mark-offset - mark-length, 0pt)
  horizontal(width + mark-offset, 0pt)
  horizontal(-mark-offset - mark-length, height)
  horizontal(width + mark-offset, height)
  vertical(0pt, -mark-offset - mark-length)
  vertical(width, -mark-offset - mark-length)
  vertical(0pt, height + mark-offset)
  vertical(width, height + mark-offset)
}

// calibration draws a rule of known length, to check the sheet printed at
// actual size.
#let calibration() = {
  let label(body) = text(font: sans, size: 6pt, fill: mark-colour)[#body]
  block[
    #box(width: 1in, height: 4pt, stroke: (bottom: 0.4pt + mark-colour, x: 0.4pt + mark-colour))
    #h(4pt)
    #label[1 in]
    #h(10pt)
    #box(width: 5cm, height: 4pt, stroke: (bottom: 0.4pt + mark-colour, x: 0.4pt + mark-colour))
    #h(4pt)
    #label[5 cm at actual size]
  ]
}

// impose lays a flat sequence of cards onto as many sheets as they need,
// preserving their order.
#let impose(cards, plan, card-width, card-height) = {
  let rotated = plan.at("rotated", default: false)
  let cell-width = if rotated { card-height } else { card-width }
  let cell-height = if rotated { card-width } else { card-height }
  let gutter = length-of(plan.gutter)
  let per-sheet = plan.columns * plan.rows
  let sheets = ()
  let index = 0
  while index < cards.len() {
    let batch = cards.slice(index, calc.min(index + per-sheet, cards.len()))
    sheets.push({
      for (slot, card) in batch.enumerate() {
        let column = calc.rem(slot, plan.columns)
        let row = int(slot / plan.columns)
        let x = length-of(plan.margin_x) + column * (cell-width + gutter)
        let y = length-of(plan.margin_y) + row * (cell-height + gutter)
        if plan.at("crop_marks", default: true) {
          place(dx: x, dy: y, crop-marks(cell-width, cell-height))
        }
        // Positioned rather than aligned, which would also align the card's
        // text.
        if rotated {
          place(
            dx: x + (cell-width - card-width) / 2,
            dy: y + (cell-height - card-height) / 2,
            rotate(-90deg, reflow: false, card),
          )
        } else {
          place(dx: x, dy: y, card)
        }
      }
      if plan.at("calibration_mark", default: true) {
        place(bottom + left, dy: -0.16in, calibration())
      }
    })
    index += per-sheet
  }
  sheets
}
