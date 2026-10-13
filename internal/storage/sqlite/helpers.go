package sqlite

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/savorum/savorum/internal/recipe"
	sqliteDriver "modernc.org/sqlite"
	sqliteCode "modernc.org/sqlite/lib"
)

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return value
}

func nullableSessionID(value *string) any {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}

	return *value
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func normalizeName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func nullableUint64(value *uint64) any {
	if value == nil {
		return nil
	}

	return *value
}

func nullableRating(value *uint8) any {
	if value == nil {
		return nil
	}

	return *value
}

func nullableRational(value *recipe.Rational) any {
	if value == nil {
		return nil
	}

	return value.String()
}

func rationalOrNil(value sql.NullString) *recipe.Rational {
	if !value.Valid || value.String == "" {
		return nil
	}

	converted := recipe.Rational(value.String)

	return &converted
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}

	return formatTime(*value)
}

func isConstraint(err error) bool {
	var failure *sqliteDriver.Error
	return errors.As(err, &failure) && failure.Code()&0xff == sqliteCode.SQLITE_CONSTRAINT
}

// countOrNil converts a nullable stored counter into the domain's unsigned
// form, clamping a negative value rather than letting it wrap.
func countOrNil(value sql.NullInt64) *uint64 {
	if !value.Valid {
		return nil
	}

	converted := uint64(max(value.Int64, 0))

	return &converted
}
