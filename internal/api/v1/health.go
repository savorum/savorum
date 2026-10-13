package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type healthOutput struct {
	Body struct {
		Status string `json:"status" enum:"ok"`
	}
}

func registerHealth(api huma.API, health HealthChecker) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/api/v1/health",
		Summary:     "Check health",
		Tags:        []string{"Instance"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		checkContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := health.Ping(checkContext); err != nil {
			return nil, problem(http.StatusServiceUnavailable, "service-unavailable", "Savorum is not ready to serve requests")
		}

		output := &healthOutput{}
		output.Body.Status = "ok"

		return output, nil
	})
}
