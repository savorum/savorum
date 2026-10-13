// Package selection describes bounded display projections for pickers.
package selection

// BookSelectionSummary distinguishes a selection without loading book membership.
type BookSelectionSummary struct {
	ID           string `json:"id"`
	Revision     uint64 `json:"revision"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	Subtitle     string `json:"subtitle,omitempty"`
	Author       string `json:"author,omitempty"`
	Editor       string `json:"editor,omitempty"`
	Edition      string `json:"edition,omitempty"`
	CoverAssetID string `json:"cover_asset_id,omitempty"`
}

// SavedMealSummary describes a combination; dishes are fetched on selection.
type SavedMealSummary struct {
	ID        string   `json:"id"`
	Revision  uint64   `json:"revision"`
	Name      string   `json:"name"`
	DishNames []string `json:"dish_names"`
	DishCount int      `json:"dish_count"`
}
