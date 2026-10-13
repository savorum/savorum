package webhook

import "fmt"

func operationError(operation string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("webhook %s: %w", operation, err)
}
