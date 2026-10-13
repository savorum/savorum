package architecture_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestCorePackagesDoNotDependOnOuterLayers(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)

	rules := []struct {
		directory string
		forbidden []string
	}{
		{directory: "internal/export", forbidden: []string{"github.com/savorum/savorum/internal/storage", "database/sql", "plugin"}},
		{directory: "internal/federation", forbidden: []string{"github.com/savorum/savorum/internal/storage", "database/sql", "plugin"}},
		{directory: "internal/exchange", forbidden: []string{"github.com/savorum/savorum/internal/storage", "database/sql", "plugin"}},

		{
			directory: "internal/recipe",
			forbidden: []string{"github.com/danielgtaylor/huma", "github.com/savorum/savorum/internal/api", "github.com/savorum/savorum/internal/library", "github.com/savorum/savorum/internal/storage", "database/sql", "net/http"},
		},
		{
			directory: "internal/library",
			forbidden: []string{"github.com/danielgtaylor/huma", "github.com/savorum/savorum/internal/api", "github.com/savorum/savorum/internal/storage", "database/sql", "net/http"},
		},
		{
			// Source adapters interpret bytes into candidates. They cannot reach
			// persistence or library services; exchange owns preview and commit.
			directory: "internal/importer",
			forbidden: []string{"github.com/savorum/savorum/internal/library", "github.com/savorum/savorum/internal/storage", "database/sql"},
		},
		{
			directory: "internal/recipecodec",
			forbidden: []string{"github.com/danielgtaylor/huma", "github.com/savorum/savorum/internal/api", "github.com/savorum/savorum/internal/library", "github.com/savorum/savorum/internal/storage", "github.com/savorum/savorum/internal/units", "database/sql", "net/http"},
		},
		{
			directory: "internal/recipecodec/v1",
			forbidden: []string{"github.com/savorum/savorum/internal", "database/sql", "net/http"},
		},
		{
			// Printing must not know the typesetting engine, the API, or storage.
			directory: "internal/print",
			forbidden: []string{"github.com/danielgtaylor/huma", "github.com/savorum/savorum/internal/api", "github.com/savorum/savorum/internal/render/typst", "github.com/savorum/savorum/internal/storage", "database/sql", "net/http"},
		},
		{
			// The renderer never reaches into the library, the API, or storage.
			directory: "internal/render/typst",
			forbidden: []string{"github.com/danielgtaylor/huma", "github.com/savorum/savorum/internal/api", "github.com/savorum/savorum/internal/library", "github.com/savorum/savorum/internal/storage", "database/sql", "net/http", "os/exec"},
		},
	}
	for _, rule := range rules {
		t.Run(rule.directory, func(t *testing.T) {
			t.Parallel()

			files, err := filepath.Glob(filepath.Join(root, rule.directory, "*.go"))
			if err != nil {
				t.Fatal(err)
			}

			for _, filename := range files {
				if strings.HasSuffix(filename, "_test.go") {
					continue
				}

				parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
				if err != nil {
					t.Fatal(err)
				}

				for _, imported := range parsed.Imports {
					path, err := strconv.Unquote(imported.Path.Value)
					if err != nil {
						t.Fatal(err)
					}

					for _, forbidden := range rule.forbidden {
						if path == forbidden || strings.HasPrefix(path, forbidden+"/") {
							t.Errorf("%s imports forbidden outer-layer package %s", filename, path)
						}
					}
				}
			}
		})
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("determine test source location")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
