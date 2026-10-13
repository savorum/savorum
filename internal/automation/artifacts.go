package automation

import (
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// sweepArtifacts also removes files orphaned by a crash between expiry and
// deletion. Each database has a private directory; paths never come from clients.
func (s *Service) sweepArtifacts() (int64, error) {
	directory, err := s.artifacts.Open(".")
	if err != nil {
		return 0, operationError("sweepArtifacts", err)
	}
	defer directory.Close()

	var total int64

	for {
		entries, readErr := directory.ReadDir(100)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "job_") || !strings.HasSuffix(entry.Name(), ".artifact") {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				return 0, operationError("sweepArtifacts", err)
			}

			if time.Since(info.ModTime()) > 24*time.Hour {
				if err = s.artifacts.Remove(entry.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
					return 0, operationError("sweepArtifacts", err)
				}
			} else {
				total += info.Size()
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}

			return 0, operationError("sweepArtifacts", readErr)
		}
	}
}
