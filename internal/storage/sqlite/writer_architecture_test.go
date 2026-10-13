package sqlite

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestRuntimeDatabaseWritersUseFence(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "writer.go" || name == "store.go" {
			continue
		}

		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			if selector, ok := n.(*ast.SelectorExpr); ok && selector.Sel.Name == "raw" {
				t.Errorf("%s accesses unfenced database pool", name)
			}

			return true
		})
	}

	var store Store
	if store.db != nil {
		t.Fatal("unexpected initialized test store")
	}
	// This assignment fails to compile if Store's database field becomes *sql.DB.
	store.db = &writerDB{}
}
