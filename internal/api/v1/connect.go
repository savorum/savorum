package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/connect"
	"github.com/savorum/savorum/internal/identity"
)

type connectOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         connect.Status
}

type connectInput struct{ Body connect.Setup }

// RegisterConnect adds instance administrator routes for Connect.
func RegisterConnect(api huma.API, service *connect.Service, authEnabled bool) {
	authorize := func(ctx context.Context) error {
		principal, ok := identity.FromContext(ctx)
		if !authEnabled || !ok || !principal.InstanceAdmin || principal.System {
			return huma.Error403Forbidden("Instance administrator access is required")
		}

		if service == nil {
			return huma.Error503ServiceUnavailable("Connect is unavailable")
		}

		return nil
	}
	result := func() *connectOutput { return &connectOutput{CacheControl: "no-store", Body: service.Status()} }

	huma.Register(api, huma.Operation{OperationID: "get-connect", Method: http.MethodGet, Path: "/api/v1/instance/connect", Summary: "Get Connect status", Tags: []string{"Instance"}, Description: "Requires an authenticated instance administrator browser session. Mutations require X-CSRF-Token. Personal API tokens are not accepted. Enrollment secrets are never returned.", Security: []map[string][]string{{"browserSession": {}}}, Errors: []int{401, 403, 503}}, func(ctx context.Context, _ *struct{}) (*connectOutput, error) {
		if err := authorize(ctx); err != nil {
			return nil, err
		}

		return result(), nil
	})
	huma.Register(api, huma.Operation{OperationID: "replace-connect", Method: http.MethodPut, Path: "/api/v1/instance/connect", Summary: "Configure Connect", Tags: []string{"Instance"}, Description: "Requires an authenticated instance administrator browser session. Mutations require X-CSRF-Token. Personal API tokens are not accepted. Enrollment secrets are never returned.", Security: []map[string][]string{{"browserSession": {}}}, MaxBodyBytes: 8192, Errors: []int{400, 401, 403, 503}}, func(ctx context.Context, input *connectInput) (*connectOutput, error) {
		if err := authorize(ctx); err != nil {
			return nil, err
		}

		if err := service.Configure(input.Body); err != nil {
			return nil, huma.Error400BadRequest("Connect setup could not be claimed or saved. Check the relay connection and use a fresh setup document.")
		}

		return result(), nil
	})
	huma.Register(api, huma.Operation{OperationID: "delete-connect", Method: http.MethodDelete, Path: "/api/v1/instance/connect", Summary: "Disconnect remote access", Tags: []string{"Instance"}, Description: "Requires an authenticated instance administrator browser session. Mutations require X-CSRF-Token. Personal API tokens are not accepted. Enrollment secrets are never returned.", Security: []map[string][]string{{"browserSession": {}}}, Errors: []int{401, 403, 503}}, func(ctx context.Context, _ *struct{}) (*connectOutput, error) {
		if err := authorize(ctx); err != nil {
			return nil, err
		}

		if err := service.Disconnect(); err != nil {
			return nil, huma.Error503ServiceUnavailable("Connect could not revoke remote access. Check the relay connection and retry.")
		}

		return result(), nil
	})
}
