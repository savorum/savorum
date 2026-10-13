package compiler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
)

// infiniteLoopWASM is this module in binary form:
//
//	(module
//	  (func (export "infinite_loop")
//	    (loop (br 0))))
var infiniteLoopWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // magic, version
	0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type section: func () -> ()
	0x03, 0x02, 0x01, 0x00, // function section: one function of type 0
	0x07, 0x11, 0x01, 0x0d, // export section: one export, name length 13
	'i', 'n', 'f', 'i', 'n', 'i', 't', 'e', '_', 'l', 'o', 'o', 'p',
	0x00, 0x00, // kind func, index 0
	0x0a, 0x09, 0x01, 0x07, 0x00, // code section: one body, no locals
	0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b, // loop, br 0, end, end
}

func TestCompilerConfigurationRequiresBothHardLimits(t *testing.T) {
	if _, err := configuredRuntime(Config{CloseOnContextDone: true}); err == nil {
		t.Fatal("a compiler with no memory limit was accepted")
	}

	if _, err := configuredRuntime(Config{MemoryLimitPages: 1}); err == nil {
		t.Fatal("a compiler that leaves guest execution running after cancellation was accepted")
	}
}

func TestConfiguredRuntimeTerminatesGuestExecutionAtTheContextDeadline(t *testing.T) {
	configuration, err := configuredRuntime(Config{MemoryLimitPages: 1, CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}

	runtime := wazero.NewRuntimeWithConfig(context.Background(), configuration)

	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	module, err := runtime.Instantiate(context.Background(), infiniteLoopWASM)
	if err != nil {
		t.Fatal(err)
	}

	loop := module.ExportedFunction("infinite_loop")
	if loop == nil {
		t.Fatal("test module has no infinite_loop export")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	started := time.Now()

	_, err = loop.Call(ctx)
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("guest call error = %v, want context deadline termination", err)
	}

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("guest execution survived cancellation for %v", elapsed)
	}

	if !module.IsClosed() {
		t.Fatal("canceled guest module remains open")
	}
}
