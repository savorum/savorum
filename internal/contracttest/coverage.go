package contracttest

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
)

// CoverageEnvironment names the file that collects the operations whose
// responses were validated. Test processes append to it; Uncovered reads it.
const CoverageEnvironment = "SAVORUM_CONTRACT_COVERAGE"

var recording sync.Mutex

func record(operation Operation, status int) {
	path := os.Getenv(CoverageEnvironment)
	if path == "" {
		return
	}

	recording.Lock()
	defer recording.Unlock()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // The path comes from the test environment.
	if err != nil {
		return
	}

	defer file.Close()

	_, _ = fmt.Fprintf(file, "%s %s %d\n", operation.Method, operation.Path, status)
}

// Uncovered lists the operations that never produced a validated success or
// redirect response in the recorded runs.
func (c *Contract) Uncovered(path string) ([]Operation, error) {
	file, err := os.Open(path) //nolint:gosec // The path is the recorded-coverage file the caller names.
	if err != nil {
		return nil, fmt.Errorf("open coverage: %w", err)
	}

	defer file.Close()

	succeeded := map[Operation]bool{}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 3 && (strings.HasPrefix(fields[2], "2") || strings.HasPrefix(fields[2], "3")) {
			succeeded[Operation{Method: fields[0], Path: fields[1]}] = true
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read coverage: %w", err)
	}

	var missing []Operation

	for _, operation := range c.Operations() {
		if !succeeded[operation] {
			missing = append(missing, operation)
		}
	}

	slices.SortFunc(missing, func(a, b Operation) int { return strings.Compare(a.Path+" "+a.Method, b.Path+" "+b.Method) })

	return missing, nil
}
