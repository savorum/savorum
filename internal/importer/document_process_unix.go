//go:build linux || darwin

package importer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// Arguments remain positional shell parameters. File blocks use 1024 bytes,
// giving at most 8 MiB on both supported shells (512 or 1024 byte blocks).
// Soft limits precede hard limits for shells that apply -H independently.
const documentLimitScript = `ulimit -S -c 0 && ulimit -H -c 0 && ulimit -S -t 15 && ulimit -H -t 15 && ulimit -S -f 8192 && ulimit -H -f 8192 && ` + documentMemoryLimitScript + `exec "$@"`

func documentCommand(ctx context.Context, name string, arguments ...string) (*exec.Cmd, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("find document tool %s: %w", name, err)
	}

	args := []string{"-c", documentLimitScript, "savorum-document", path}
	args = append(args, arguments...)
	command := exec.CommandContext(ctx, "/bin/sh", args...) //nolint:gosec // fixed script; executable and arguments are positional parameters

	command.Env = append(os.Environ(), "LC_ALL=C", "OMP_THREAD_LIMIT=1", "OMP_NUM_THREADS=1")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return stopDocumentProcess(command) }

	return command, nil
}

func stopDocumentProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}

	if err := unix.Kill(-command.Process.Pid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("stop document process group: %w", err)
	}

	return nil
}
