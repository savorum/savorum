package print

import "fmt"

// Imposition constants are stated in points, which is the unit a page box is
// finally written in. They describe the physical realities of desktop
// printing rather than a visual preference.
const (
	// safeOuterMargin is the border most desktop printers cannot reach. A card
	// grid never enters it, and a sheet carries fewer cards rather than cards
	// pushed into an area that would be clipped.
	safeOuterMargin = 0.25 * 72
	// cardGutter separates adjacent cards so a trimmed edge belongs to exactly
	// one card and a knife has somewhere to go.
	cardGutter = 0.25 * 72
)

// planSheet works out how many exact-size cards fit on a sheet, and where. It
// never scales a card: a card that does not fit is either turned a quarter
// turn, which preserves its dimensions, or the combination is refused.
func planSheet(card, sheet Dimensions) (SheetPlan, error) {
	cardWidth, cardHeight := card.Width.Points(), card.Height.Points()
	sheetWidth, sheetHeight := sheet.Width.Points(), sheet.Height.Points()

	upright, uprightOK := fitGrid(cardWidth, cardHeight, sheetWidth, sheetHeight)

	turned, turnedOK := fitGrid(cardHeight, cardWidth, sheetWidth, sheetHeight)
	switch {
	case uprightOK && (!turnedOK || upright.count() >= turned.count()):
		return sheetPlanOf(upright, false), nil
	case turnedOK:
		return sheetPlanOf(turned, true), nil
	default:
		return SheetPlan{}, fmt.Errorf(
			"%w: a %s card does not fit on %s inside a printable margin",
			ErrInvalidOptions, card.Width.String()+" by "+card.Height.String(), sheet.Width.String()+" by "+sheet.Height.String(),
		)
	}
}

type grid struct {
	columns int
	rows    int
	marginX float64
	marginY float64
}

func (g grid) count() int { return g.columns * g.rows }

// fitGrid packs as many cards of the given footprint onto the sheet as the
// printable area allows, then centres them.
func fitGrid(cardWidth, cardHeight, sheetWidth, sheetHeight float64) (grid, bool) {
	columns := fitCount(cardWidth, sheetWidth)

	rows := fitCount(cardHeight, sheetHeight)
	if columns < 1 || rows < 1 {
		return grid{}, false
	}

	usedWidth := float64(columns)*cardWidth + float64(columns-1)*cardGutter
	usedHeight := float64(rows)*cardHeight + float64(rows-1)*cardGutter

	return grid{
		columns: columns,
		rows:    rows,
		marginX: (sheetWidth - usedWidth) / 2,
		marginY: (sheetHeight - usedHeight) / 2,
	}, true
}

func fitCount(card, sheet float64) int {
	available := sheet - 2*safeOuterMargin
	if card <= 0 || available < card {
		return 0
	}
	// Solving n*card + (n-1)*gutter <= available for whole n.
	return int((available + cardGutter) / (card + cardGutter))
}

func sheetPlanOf(g grid, rotated bool) SheetPlan {
	return SheetPlan{
		Columns:         g.columns,
		Rows:            g.rows,
		Gutter:          points(cardGutter),
		MarginX:         points(g.marginX),
		MarginY:         points(g.marginY),
		Rotated:         rotated,
		CropMarks:       true,
		CalibrationMark: true,
	}
}

// points states a computed length for the typesetter. Card dimensions keep
// the unit they are defined in instead.
func points(value float64) Length {
	return Length{Value: value, Unit: "pt"}
}
