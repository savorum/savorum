package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/storagebudget"
)

// CustomerStorageStatus exposes measurements without internal paths or diagnostics.
type CustomerStorageStatus struct {
	storagebudget.Status
	SupportURL string `json:"support_url,omitempty"`
	Hosted     bool   `json:"hosted"`
}

type storageStatusOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         CustomerStorageStatus
}

// RegisterStorageStatus shares the runtime contract with the schema generator.
// Authentication is enforced by the application's existing account middleware.
func RegisterStorageStatus(api huma.API, read func(context.Context) CustomerStorageStatus) {
	huma.Register(api, huma.Operation{
		OperationID: "get-storage-status", Method: http.MethodGet, Path: "/api/v1/storage-status",
		Summary: "Observe storage capacity", Tags: []string{"Instance"},
		Description: "Authenticated, bounded read of current combined hosted usage and filesystem headroom. Unknown measurements are omitted. This observation does not change write admission or start background polling.",
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*storageStatusOutput, error) {
		report := CustomerStorageStatus{State: "unknown"}
		if read != nil {
			report = read(ctx)
		}

		return &storageStatusOutput{CacheControl: "no-store", Body: report}, nil
	})
}
