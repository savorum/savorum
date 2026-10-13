package automation

import "fmt"

func operationError(operation string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("automation %s: %w", operation, err)
}
