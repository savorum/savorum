//go:build !linux && !darwin

package importer

import (
	"context"
	"fmt"
	"os/exec"
)

func documentCommand(context.Context, string, ...string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("bounded native document extraction requires Linux or macOS")
}

func stopDocumentProcess(*exec.Cmd) error {
	return nil
}
