package sqlite

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
	sqliteDriver "modernc.org/sqlite"
)

var registerSearchFunctions = sync.OnceValue(func() error {
	for _, function := range []struct {
		name string
		call func(string) (string, error)
	}{
		{"savorum_author_name", func(value string) (string, error) { return library.NormalizeAuthorName(value), nil }},
		{"savorum_author_accent", func(value string) (string, error) { return library.AuthorAccentName(value), nil }},
		{"savorum_author_keys", func(value string) (string, error) {
			encoded, err := json.Marshal(library.AuthorSearchKeys(value))
			if err != nil {
				return "", fmt.Errorf("encode author search keys: %w", err)
			}

			return string(encoded), nil
		}},
		{"savorum_name_terms", func(value string) (string, error) {
			encoded, err := json.Marshal(library.NameSearchTerms(value))
			if err != nil {
				return "", fmt.Errorf("encode name search terms: %w", err)
			}

			return string(encoded), nil
		}},
		{"savorum_ingredient_names", ingredientSearchNames},
		{"savorum_duration", func(value string) (string, error) {
			duration, ok := recipe.ParseDuration(value)
			if !ok {
				return "", nil
			}

			return strconv.FormatInt(int64(duration), 10), nil
		}},
	} {
		if err := sqliteDriver.RegisterDeterministicScalarFunction(function.name, 1, func(_ *sqliteDriver.FunctionContext, args []driver.Value) (driver.Value, error) {
			value, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("search function requires a string")
			}

			return function.call(value)
		}); err != nil {
			return fmt.Errorf("register %s: %w", function.name, err)
		}
	}

	return nil
})
