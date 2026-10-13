package recipe

// DisplayYield returns the yield shown first and scaled by: preferred when it
// exists, else the first numeric yield, else the first.
func (d Document) DisplayYield(preferred *int) (*Yield, int) {
	if len(d.Yields) == 0 {
		return nil, -1
	}

	if preferred != nil && *preferred >= 0 && *preferred < len(d.Yields) {
		return &d.Yields[*preferred], *preferred
	}

	for index := range d.Yields {
		if d.Yields[index].Quantity != nil && d.Yields[index].Quantity.Kind != QuantityOpaque {
			return &d.Yields[index], index
		}
	}

	return &d.Yields[0], 0
}

// FirstYield returns the first authored yield, or nil.
func (d Document) FirstYield() *Yield {
	if len(d.Yields) == 0 {
		return nil
	}

	return &d.Yields[0]
}
