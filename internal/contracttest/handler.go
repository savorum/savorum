package contracttest

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// Handler checks every API response the wrapped handler writes against the
// contract and fails the test at cleanup, so the handler's behavior is unchanged.
func Handler(t testing.TB, next http.Handler) http.Handler {
	t.Helper()

	contract, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	var (
		mutex    sync.Mutex
		failures []string
	)

	t.Cleanup(func() {
		mutex.Lock()
		defer mutex.Unlock()

		for _, failure := range failures {
			t.Error(failure)
		}
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &recorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		operation, ok := contract.Match(r.Method, r.URL.Path)
		if !ok {
			return
		}

		if strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/event-stream") {
			record(operation, recorder.status)

			return
		}

		if err := contract.Validate(operation, recorder.status, recorder.Header().Get("Content-Type"), recorder.body.Bytes()); err != nil {
			mutex.Lock()

			failures = append(failures, fmt.Sprintf("%s %s -> %d: %v", r.Method, r.URL.Path, recorder.status, err))
			mutex.Unlock()
		}
	})
}

type recorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

// WriteHeader keeps the first status, as net/http does.
func (r *recorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}

	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Flush() {
	r.wroteHeader = true

	http.NewResponseController(r.ResponseWriter).Flush() //nolint:errcheck,gosec // The wrapped writer may not flush.
}

func (r *recorder) Write(data []byte) (int, error) {
	r.wroteHeader = true
	r.body.Write(data)

	written, err := r.ResponseWriter.Write(data)
	if err != nil {
		return written, fmt.Errorf("write response: %w", err)
	}

	return written, nil
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
