package webui

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCompressedAssetsPreserveTheirTypeAndCachePolicy(t *testing.T) {
	t.Parallel()

	const body = "export const greeting = 'hello';"

	var compressed bytes.Buffer

	zipper := gzip.NewWriter(&compressed)
	if _, err := zipper.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}

	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(fstest.MapFS{
		"index.html":                {Data: []byte("<title>shell</title>")},
		"assets/app-aB12_C34.js":    {Data: []byte(body)},
		"assets/app-aB12_C34.js.gz": {Data: compressed.Bytes()},
		"sw.js":                     {Data: []byte(body)},
	})

	for _, encoding := range []string{"gzip", "br, gzip", "gzip;q=0.5", "*", "GZIP"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/assets/app-aB12_C34.js", nil)
		request.Header.Set("Accept-Encoding", encoding)

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusOK || response.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(response.Header().Get("Content-Type"), "javascript") {
			t.Fatalf("%s: status=%d headers=%v", encoding, response.Code, response.Header())
		}

		if response.Header().Get("Vary") != "Accept-Encoding" || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("%s: cache headers=%v", encoding, response.Header())
		}

		reader, err := gzip.NewReader(response.Body)
		if err != nil {
			t.Fatal(err)
		}

		decoded, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}

		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}

		if string(decoded) != body {
			t.Fatalf("decoded body = %q", decoded)
		}
	}

	for _, encoding := range []string{"", "br", "gzip;q=0", "gzip;q=0, *;q=1", "gzip;q=bad", "gzip;q=2"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/assets/app-aB12_C34.js", nil)
		request.Header.Set("Accept-Encoding", encoding)

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)

		if response.Header().Get("Content-Encoding") != "" || response.Body.String() != body {
			t.Fatalf("%s: headers=%v body=%q", encoding, response.Header(), response.Body.String())
		}
	}

	request := httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/assets/app-aB12_C34.js", nil)
	request.Header.Set("Accept-Encoding", "gzip")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Body.Len() != 0 || response.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("HEAD: headers=%v body=%q", response.Header(), response.Body.String())
	}

	for _, target := range []string{"/", "/recipes/example", "/sw.js", "/assets/missing-12345678.js", "/assets/app-aB12_C34.js.gz"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))

		if response.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s must revalidate: %v", target, response.Header())
		}
	}
}

func TestContentEncodingNegotiation(t *testing.T) {
	t.Parallel()

	const asset = "assets/app-aB12_C34.js"

	for _, test := range []struct {
		name       string
		header     string
		available  []string
		want       string
		wantStatus int
	}{
		{name: "Brotli wins equal preference", header: "gzip, deflate, br, zstd", available: []string{"br", "gzip"}, want: "br"},
		{name: "gzip wins higher preference", header: "br;q=0.4, gzip;q=0.8", available: []string{"br", "gzip"}, want: "gzip"},
		{name: "Brotli wins higher preference", header: "br;q=0.8, gzip;q=0.4", available: []string{"br", "gzip"}, want: "br"},
		{name: "Brotli is case insensitive", header: "BR;Q=0.5", available: []string{"br"}, want: "br"},
		{name: "gzip fallback", header: "br, gzip", available: []string{"gzip"}, want: "gzip"},
		{name: "Brotli only", header: "br, gzip", available: []string{"br"}, want: "br"},
		{name: "no variants", header: "br, gzip"},
		{name: "wildcard", header: "*", available: []string{"br", "gzip"}, want: "br"},
		{name: "Brotli excluded", header: "br;q=0, *;q=1", available: []string{"br", "gzip"}, want: "gzip"},
		{name: "explicit beats wildcard", header: "br;q=0.1, *;q=0.8", available: []string{"br", "gzip"}, want: "gzip"},
		{name: "identity preferred", header: "br;q=0.5, identity;q=1", available: []string{"br", "gzip"}},
		{name: "identity excluded", header: "br, identity;q=0", available: []string{"br", "gzip"}, want: "br"},
		{name: "absent header", available: []string{"br", "gzip"}},
		{name: "unknown encoding", header: "zstd", available: []string{"br", "gzip"}},
		{name: "both excluded", header: "br;q=0, gzip;q=0", available: []string{"br", "gzip"}},
		{name: "invalid weight", header: "br;q=bad, gzip", available: []string{"br", "gzip"}, want: "gzip"},
		{name: "out of range weight", header: "br;q=2, gzip", available: []string{"br", "gzip"}, want: "gzip"},
		{name: "not a number", header: "br;q=NaN, gzip", available: []string{"br", "gzip"}, want: "gzip"},
		{name: "nothing acceptable", header: "*;q=0", available: []string{"br", "gzip"}, wantStatus: http.StatusNotAcceptable},
		{name: "identity overrides wildcard", header: "*;q=0, identity;q=1", available: []string{"br", "gzip"}},
		{name: "available encoding excluded", header: "br;q=0, gzip;q=0, identity;q=0", available: []string{"br", "gzip"}, wantStatus: http.StatusNotAcceptable},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			directory := fstest.MapFS{
				"index.html": {Data: []byte("shell")},
				asset:        {Data: []byte("identity")},
			}

			for _, encoding := range test.available {
				extension := map[string]string{"br": ".br", "gzip": ".gz"}[encoding]
				directory[asset+extension] = &fstest.MapFile{Data: []byte(encoding)}
			}

			handler := newHandler(directory)

			for _, method := range []string{http.MethodGet, http.MethodHead} {
				request := httptest.NewRequestWithContext(t.Context(), method, "/"+asset, nil)
				for value := range strings.SplitSeq(test.header, ",") {
					request.Header.Add("Accept-Encoding", value)
				}

				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)

				wantStatus := test.wantStatus
				if wantStatus == 0 {
					wantStatus = http.StatusOK
				}

				if response.Code != wantStatus || response.Header().Get("Content-Encoding") != test.want {
					t.Fatalf("%s: status=%d headers=%v", method, response.Code, response.Header())
				}

				if len(test.available) > 0 && response.Header().Get("Vary") != "Accept-Encoding" {
					t.Fatalf("%s: missing Vary: %v", method, response.Header())
				}

				if wantStatus != http.StatusOK {
					if response.Header().Get("Cache-Control") != "no-cache" {
						t.Fatalf("%s: error must revalidate: %v", method, response.Header())
					}

					continue
				}

				if !strings.Contains(response.Header().Get("Content-Type"), "javascript") || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
					t.Fatalf("%s: incorrect asset headers: %v", method, response.Header())
				}

				wantBody := test.want
				if wantBody == "" {
					wantBody = "identity"
				}

				if method == http.MethodHead {
					wantBody = ""
				}

				if response.Body.String() != wantBody {
					t.Fatalf("%s: body=%q want=%q", method, response.Body.String(), wantBody)
				}
			}
		})
	}
}

func TestCompressedShellServesDeepLinks(t *testing.T) {
	t.Parallel()

	handler := newHandler(fstest.MapFS{
		"index.html":    {Data: []byte("shell")},
		"index.html.gz": {Data: []byte("compressed shell")},
		"index.html.br": {Data: []byte("compressed shell")},
	})

	for _, encoding := range []string{"br", "gzip"} {
		for _, target := range []string{"/", "/index.html", "/recipes/example", "/index.html.br", "/index.html.gz"} {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
			request.Header.Set("Accept-Encoding", encoding)

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK || response.Body.String() != "compressed shell" || response.Header().Get("Content-Type") != "text/html; charset=utf-8" || response.Header().Get("Cache-Control") != "no-cache" {
				t.Fatalf("%s: status=%d headers=%v body=%q", target, response.Code, response.Header(), response.Body.String())
			}

			if response.Header().Get("Content-Encoding") != encoding {
				t.Fatalf("%s: encoding=%q want=%q", target, response.Header().Get("Content-Encoding"), encoding)
			}
		}
	}
}

func TestAnUnbuiltFrontendSaysSoAndLeavesTheAPIAlone(t *testing.T) {
	t.Parallel()

	handler := newHandler(fstest.MapFS{".gitkeep": {}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/recipes", nil))

	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "just build") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestPlaceholderFilesAreNotServed(t *testing.T) {
	t.Parallel()

	handler := newHandler(fstest.MapFS{
		".gitkeep":   {},
		"index.html": {Data: []byte("<title>shell</title>")},
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/.gitkeep", nil))

	if !strings.Contains(response.Body.String(), "shell") {
		t.Fatalf("placeholder request served %q", response.Body.String())
	}
}

func TestHandlerAnswersMissingDocumentsWithNotFound(t *testing.T) {
	t.Parallel()

	handler := newHandler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}})

	for target, want := range map[string]int{
		"/recipes/example":           http.StatusOK,
		"/llms.txt":                  http.StatusNotFound,
		"/README.md":                 http.StatusNotFound,
		"/.well-known/security.txt":  http.StatusNotFound,
		"/.well-known/openid-config": http.StatusNotFound,
		"/api":                       http.StatusNotFound,
		"/api/v2/recipes":            http.StatusNotFound,
		"/openapi.yaml":              http.StatusNotFound,
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))

		if response.Code != want {
			t.Fatalf("%s: status %d, want %d", target, response.Code, want)
		}
	}
}
