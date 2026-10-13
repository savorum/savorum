package shopping

func reviewDifferences(requirements []ShoppingRequirement, list ShoppingList, input ShoppingReviewInput) []ShoppingSource {
	current := map[string]bool{}

	for index := range requirements {
		row := &requirements[index]
		current[row.ID] = true
		previous, ok := list.Requirements[row.ID]

		selection := list.Selections[row.ID]
		if !ok || !selection.Selected {
			continue
		}

		for _, choice := range previous.Choices {
			if choice.ID != selection.ChoiceID {
				continue
			}

			row.PreviousName = choice.Name
			row.PreviousQuantity = choice.Quantity
		}
	}

	removed := []ShoppingSource{}

	for _, item := range list.Items {
		if item.Bought || item.Deleted {
			continue
		}

		for _, source := range item.Sources {
			if affectedSource(input, source) && !current[source.ID] {
				removed = append(removed, source)
			}
		}
	}

	return removed
}
