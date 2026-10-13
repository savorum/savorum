package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

func TestConstraintClassificationUsesDriverCode(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, query := range []string{`CREATE TABLE checks (id INTEGER PRIMARY KEY)`, `INSERT INTO checks VALUES (1)`} {
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}

	_, err = db.ExecContext(t.Context(), `INSERT INTO checks VALUES (1)`)
	if !isConstraint(fmt.Errorf("保存に失敗: %w", err)) {
		t.Fatalf("constraint not classified: %v", err)
	}

	if isConstraint(errors.New("constraint network unavailable")) || isConstraint(nil) {
		t.Fatal("classified diagnostic text")
	}
}
