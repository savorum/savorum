// Package discovery lets a client that knows only an instance URL find the
// API description and the guide this build ships with.
package discovery

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/buildinfo"
	"github.com/savorum/savorum/internal/httpsecurity"
)

// Paths of the documents this package serves.
const (
	GuidePath   = "/llms.txt"
	AgentsPath  = "/AGENTS.md"
	SpecPath    = "/openapi.json"
	CatalogPath = "/.well-known/api-catalog"
	TokensPath  = "/account/tokens"
)

//go:embed guide.md
var guide string

// specPlaceholder stands in for the request origin in the cached spec.
const specPlaceholder = "https://origin.savorum.invalid"

type originKey struct{}

// Origin returns the public origin the discovery handler resolved for this request.
func Origin(ctx context.Context) string {
	origin, _ := ctx.Value(originKey{}).(string)
	return origin
}

// Options configures the discovery handler.
type Options struct {
	// PublicURL returns the configured public URL, or an empty string.
	PublicURL func(context.Context) string
	// Spec returns the finished OpenAPI document. It is read on first use.
	Spec func() *huma.OpenAPI
}

// Handler serves the guide, the API catalog, and the request-specific OpenAPI
// document, and adds discovery links to every response from next.
func Handler(next http.Handler, options Options) http.Handler {
	spec := sync.OnceValue(func() []byte {
		if options.Spec == nil {
			return nil
		}

		document := *options.Spec()
		document.Servers = []*huma.Server{{URL: specPlaceholder, Description: "This instance."}}
		document.ExternalDocs = &huma.ExternalDocs{Description: "Guide for agents and integrations", URL: specPlaceholder + GuidePath}

		encoded, err := json.Marshal(&document)
		if err != nil {
			return nil
		}

		return encoded
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configured := ""
		if options.PublicURL != nil {
			configured = options.PublicURL(r.Context())
		}

		origin := httpsecurity.PublicOrigin(r, configured)
		r = r.WithContext(context.WithValue(r.Context(), originKey{}, origin))
		w.Header().Set("Link", Links(origin))

		read := r.Method == http.MethodGet || r.Method == http.MethodHead

		switch {
		case read && (r.URL.Path == GuidePath || r.URL.Path == AgentsPath):
			serveGuide(w, r, origin)
		case read && r.URL.Path == CatalogPath:
			serveCatalog(w, r, origin)
		case read && r.URL.Path == SpecPath && spec() != nil:
			w.Header().Set("Content-Type", "application/openapi+json")
			w.Header().Set("Cache-Control", "no-cache")

			if r.Method == http.MethodGet {
				_, _ = w.Write(bytes.ReplaceAll(spec(), []byte(specPlaceholder), jsonString(origin))) //nolint:gosec // JSON with a JSON-escaped origin, not HTML
			}
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// Links is the Link header value that points at the API description, the
// guide, and the API catalog.
func Links(origin string) string {
	return strings.Join([]string{
		"<" + origin + SpecPath + `>; rel="service-desc"; type="application/openapi+json"`,
		"<" + origin + GuidePath + `>; rel="service-doc"; type="text/markdown"`,
		"<" + origin + CatalogPath + `>; rel="api-catalog"`,
	}, ", ")
}

// Guide returns the guide written for one origin.
func Guide(origin string) string {
	return strings.NewReplacer("{{origin}}", origin, "{{version}}", buildinfo.CurrentVersion()).Replace(guide)
}

// AssistantGuide returns the guide for a connected MCP client, without the
// connection setup that ConnectionGuide returns.
func AssistantGuide(origin string) string {
	text := Guide(origin)
	start, end := connectionSection(text)

	return text[:start] + text[end:]
}

// ConnectionGuide returns the guide's connection setup for one origin.
func ConnectionGuide(origin string) string {
	text := Guide(origin)
	start, end := connectionSection(text)

	return text[start:end]
}

// connectionSection spans the connection setup heading up to the next heading.
func connectionSection(text string) (int, int) {
	start := strings.Index(text, "\n## Connecting without a key\n") + 1

	return start, start + 2 + strings.Index(text[start+1:], "\n## ")
}

func serveGuide(w http.ResponseWriter, r *http.Request, origin string) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte(Guide(origin)))
	}
}

type catalogLink struct {
	Href string `json:"href"`
	Type string `json:"type,omitempty"`
}

//nolint:tagliatelle // RFC 9727 link relation names
type catalogEntry struct {
	Anchor      string        `json:"anchor"`
	ServiceDesc []catalogLink `json:"service-desc"`
	ServiceDoc  []catalogLink `json:"service-doc"`
	ServiceMeta []catalogLink `json:"service-meta"`
	Status      []catalogLink `json:"status"`
}

// serveCatalog writes an RFC 9727 API catalog.
func serveCatalog(w http.ResponseWriter, r *http.Request, origin string) {
	catalog := struct {
		Linkset []catalogEntry `json:"linkset"`
	}{[]catalogEntry{{
		Anchor:      origin + "/api/v1",
		ServiceDesc: []catalogLink{{Href: origin + SpecPath, Type: "application/openapi+json"}},
		ServiceDoc:  []catalogLink{{Href: origin + GuidePath, Type: "text/markdown"}},
		ServiceMeta: []catalogLink{{Href: origin + "/api/v1/capabilities", Type: "application/json"}},
		Status:      []catalogLink{{Href: origin + "/api/v1/health", Type: "application/json"}},
	}}}

	w.Header().Set("Content-Type", `application/linkset+json; profile="https://www.rfc-editor.org/info/rfc9727"`)
	w.Header().Set("Cache-Control", "no-cache")

	if r.Method == http.MethodGet {
		if err := json.NewEncoder(w).Encode(catalog); err != nil {
			return
		}
	}
}

// jsonString is value encoded as the inside of a JSON string.
func jsonString(value string) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}

	return encoded[1 : len(encoded)-1]
}
