package account

import (
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type openAPIRouteStore struct {
	unexpectedRepository
	unexpectedPasskeyRepository
	unexpectedProvisioningRepository
}

func TestEveryAccountRouteHasAnOpenAPIContract(t *testing.T) {
	handler := New(openAPIRouteStore{}, Options{}, http.NotFoundHandler())
	api := humago.New(http.NewServeMux(), huma.DefaultConfig("test", "1"))
	RegisterOpenAPI(api)

	routes := handler.routes.Routes()
	if len(routes) == 0 {
		t.Fatal("no account routes found")
	}

	for _, route := range routes {
		item, exists := api.OpenAPI().Paths[route.Path]
		if !exists {
			t.Errorf("undocumented route %s %s", route.Method, route.Path)
			continue
		}

		methods := map[string]*huma.Operation{"get": item.Get, "post": item.Post, "put": item.Put, "patch": item.Patch, "delete": item.Delete}
		if methods[strings.ToLower(route.Method)] == nil {
			t.Errorf("undocumented method %s %s", route.Method, route.Path)
		}
	}
}
