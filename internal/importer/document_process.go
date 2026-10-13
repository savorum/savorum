package importer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	errDocumentOutputLimit = errors.New("document tool output exceeds limit")
	errDocumentBusy        = errors.New("document extraction is busy; try again")
	documentWork           = make(chan struct{}, 1)
)

func acquireDocumentWork(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	select {
	case documentWork <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-documentWork
			return nil, err
		}

		return func() { <-documentWork }, nil
	default:
		return nil, errDocumentBusy
	}
}

type documentOutput struct {
	buffer *cappedBuffer
	stop   context.CancelFunc
	err    error
}

func (out *documentOutput) Write(data []byte) (int, error) {
	n, err := out.buffer.Write(data)
	if err != nil {
		out.err = err
		out.stop()
	}

	return n, err
}

// Native limits precede exec. Pipe overflow and cancellation kill the process
// group; WaitDelay also bounds a descendant retaining an inherited pipe.
func runDocumentTool(ctx context.Context, out *cappedBuffer, name string, arguments ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	childContext, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	command, err := documentCommand(childContext, name, arguments...)
	if err != nil {
		return err
	}

	if out == nil {
		out = &cappedBuffer{max: 64 << 10}
	}

	stderr := cappedBuffer{max: 64 << 10}
	stdoutWriter := documentOutput{buffer: out, stop: cancel}
	stderrWriter := documentOutput{buffer: &stderr, stop: cancel}
	command.Stdout = &stdoutWriter
	command.Stderr = &stderrWriter
	command.WaitDelay = 200 * time.Millisecond

	runErr := command.Run()
	stopErr := stopDocumentProcess(command)

	if stdoutWriter.err != nil {
		return fmt.Errorf("%s: %w", name, stdoutWriter.err)
	}

	if stderrWriter.err != nil {
		return fmt.Errorf("%s stderr: %w", name, stderrWriter.err)
	}

	if err := childContext.Err(); err != nil {
		return fmt.Errorf("%s extraction stopped: %w", name, err)
	}

	if runErr != nil {
		if detail := strings.TrimSpace(stderr.b.String()); detail != "" {
			return fmt.Errorf("%s could not read this document: %w: %s", name, runErr, detail)
		}

		return fmt.Errorf("%s could not read this document: %w", name, runErr)
	}

	if stopErr != nil {
		return stopErr
	}

	return nil
}
