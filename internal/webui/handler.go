// Package webui serves the built frontend from the binary. The assets are
// embedded so that a Savorum instance is one file to deploy, with no static
// directory to keep in step with the server that references it.
package webui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// The build output is not committed. `all:` admits the tracked .gitkeep, so a
// checkout without a frontend build still compiles.
//
//go:embed all:dist
var embedded embed.FS

var fingerprintedAsset = regexp.MustCompile(`^assets/.+-[A-Za-z0-9_-]{8}\.[a-z0-9]+$`)

const unbuiltPage = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<title>Savorum</title>
<p>The web interface is not built into this Savorum binary. Run <code>just build</code> to build it. The API is available.</p>
</html>
`

// Handler serves the embedded web application.
func Handler() http.Handler {
	directory, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("create embedded web filesystem: " + err.Error())
	}

	return newHandler(directory)
}

func newHandler(directory fs.FS) http.Handler {
	_, missing := fs.Stat(directory, "index.html")
	files := http.FileServer(http.FS(directory))

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-cache")

		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

			return
		}

		if missing != nil {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte(unbuiltPage))

			return
		}

		cleaned := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if cleaned == "." {
			cleaned = "index.html"
		}

		if _, err := fs.Stat(directory, cleaned); err != nil || strings.HasPrefix(path.Base(cleaned), ".") || strings.HasSuffix(cleaned, ".gz") || strings.HasSuffix(cleaned, ".br") {
			cleaned = "index.html"
		}

		if fingerprintedAsset.MatchString(cleaned) {
			writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		if cleaned == "manifest.webmanifest" {
			writer.Header().Set("Content-Type", "application/manifest+json")
		}

		clone := request.Clone(request.Context())

		if cleaned == "index.html" {
			clone.URL.Path = "/"
		}

		variants := map[string]string{}

		for encoding, extension := range map[string]string{"br": ".br", "gzip": ".gz"} {
			if _, err := fs.Stat(directory, cleaned+extension); err == nil {
				variants[encoding] = cleaned + extension
			}
		}

		if len(variants) > 0 {
			writer.Header().Add("Vary", "Accept-Encoding")
		}

		encoding, acceptable := preferredEncoding(strings.Join(request.Header.Values("Accept-Encoding"), ","), variants)
		if !acceptable {
			writer.Header().Set("Cache-Control", "no-cache")
			http.Error(writer, http.StatusText(http.StatusNotAcceptable), http.StatusNotAcceptable)

			return
		}

		if encoding != "" {
			writer.Header().Set("Content-Encoding", encoding)

			if writer.Header().Get("Content-Type") == "" {
				writer.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(cleaned)))
			}

			clone.URL.Path = "/" + variants[encoding]
		}

		files.ServeHTTP(writer, clone)
	})
}

func preferredEncoding(header string, variants map[string]string) (string, bool) {
	quality := encodingQualities(header)
	preferred, best := "", 0.0

	for _, encoding := range []string{"br", "gzip"} {
		if _, available := variants[encoding]; !available {
			continue
		}

		weight, specified := quality[encoding]
		if !specified {
			weight = quality["*"]
		}

		if weight > best {
			preferred, best = encoding, weight
		}
	}

	identity, specified := quality["identity"]
	if specified && identity > best {
		return "", true
	}

	if best > 0 {
		return preferred, true
	}

	if !specified {
		identity = 1

		if wildcard, exists := quality["*"]; exists && wildcard == 0 {
			identity = 0
		}
	}

	return "", identity > 0
}

func encodingQualities(header string) map[string]float64 {
	quality := map[string]float64{}

	for encoding := range strings.SplitSeq(header, ",") {
		parts := strings.Split(encoding, ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		weight := 1.0

		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "q") {
				parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err != nil || !(parsed >= 0 && parsed <= 1) {
					weight = 0
				} else {
					weight = parsed
				}
			}
		}

		quality[name] = weight
	}

	return quality
}
