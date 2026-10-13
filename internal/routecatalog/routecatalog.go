// Package routecatalog records method routes while serving them through net/http.
package routecatalog

import (
	"net/http"
	"slices"
	"strings"
)

// Route identifies one method-specific HTTP route.
type Route struct {
	Method string
	Path   string
}

// Mux records method routes registered on its embedded ServeMux.
type Mux struct {
	*http.ServeMux
	routes []Route
}

// New returns an empty recording mux.
func New() *Mux {
	return &Mux{ServeMux: http.NewServeMux()}
}

// HandleFunc records a method-specific pattern and registers its handler.
func (m *Mux) HandleFunc(pattern string, handler http.HandlerFunc) {
	if method, path, found := strings.Cut(pattern, " "); found {
		m.routes = append(m.routes, Route{Method: method, Path: path})
	}

	m.ServeMux.HandleFunc(pattern, handler)
}

// Routes returns the recorded method routes in registration order.
func (m *Mux) Routes() []Route {
	return slices.Clone(m.routes)
}
