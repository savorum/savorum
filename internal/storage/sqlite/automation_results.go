package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/savorum/savorum/internal/automation"
	"github.com/savorum/savorum/internal/library"
)

type storedAutomationResult struct {
	Result automation.Result `json:"result"`
	Entry  *library.Entry    `json:"entry,omitempty"`
}

func appendAutomationResults(ctx context.Context, tx *sql.Tx, id string, results []automation.Result) error {
	var completed int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM automation_results WHERE job_id=?`, id).Scan(&completed); err != nil {
		return operationError("count completed job items", err)
	}

	if completed > len(results) {
		return automation.ErrState
	}

	for i := completed; i < len(results); i++ {
		value := storedAutomationResult{Result: results[i], Entry: results[i].Entry}

		data, err := json.Marshal(value)
		if err != nil {
			return operationError("encode job result", err)
		}

		if _, err = tx.ExecContext(ctx, `INSERT INTO automation_results(job_id,position,result_json) VALUES(?,?,?)`, id, i, string(data)); err != nil {
			return operationError("checkpoint job result", err)
		}
	}

	return nil
}

// AutomationResults rechecks creator, library and token authority before reading
// immutable item snapshots. A page has at most 200 results and normally 16 MiB;
// one larger individual recipe can always make progress without being omitted.
func (s *Store) AutomationResults(ctx context.Context, id string, offset, limit int) ([]automation.Result, error) {
	if offset < 0 || limit < 1 || limit > 200 {
		return nil, automation.ErrInvalid
	}

	if _, err := s.AutomationJob(ctx, id); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT result_json FROM automation_results WHERE job_id=? AND position>=? ORDER BY position LIMIT ?`, id, offset, limit)
	if err != nil {
		return nil, operationError("read job results", err)
	}
	defer rows.Close()

	result := []automation.Result{}
	totalBytes := 0

	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, operationError("read job result", err)
		}

		if len(result) > 0 && totalBytes+len(data) > 16<<20 {
			break
		}

		totalBytes += len(data)

		var value storedAutomationResult
		if err := json.Unmarshal([]byte(data), &value); err != nil {
			return nil, operationError("decode job result", err)
		}

		value.Result.Entry = value.Entry
		result = append(result, value.Result)
	}

	return result, operationError("read job results", rows.Err())
}
