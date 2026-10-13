package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/identity"
	"github.com/savorum/savorum/internal/memoryspace"
	"github.com/savorum/savorum/internal/storagebudget"
)

// MemberCallback executes against the serving process's guarded store.
type MemberCallback func(context.Context, MemberRequest) (MemberResult, error)

// MemberHandler is exposed only on the private operator socket.
func MemberHandler(callback MemberCallback) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/member" {
			http.NotFound(w, r)
			return
		}

		var input MemberRequest

		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()

		var extra any
		if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(&extra), io.EOF) || input.Validate() != nil {
			http.Error(w, "Invalid member request", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		result, err := callback(ctx, input)
		if err != nil {
			status := http.StatusInternalServerError

			switch {
			case errors.Is(err, memoryspace.ErrResourceMemory):
				status = http.StatusServiceUnavailable
			case errors.Is(err, storagebudget.ErrCapacity), errors.Is(err, diskspace.ErrCapacity):
				status = http.StatusInsufficientStorage
			case errors.Is(err, identity.ErrConflict):
				status = http.StatusConflict
			}

			http.Error(w, http.StatusText(status), status)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
}

// RequestMember uses the private live-process channel; no database fallback exists.
func RequestMember(ctx context.Context, socket string, input MemberRequest) (MemberResult, error) {
	var result MemberResult
	if !filepath.IsAbs(socket) {
		return result, errors.New("member socket must be an absolute path")
	}

	if err := input.Validate(); err != nil {
		return result, err
	}

	data, err := json.Marshal(input)
	if err != nil {
		return result, fmt.Errorf("encode member request: %w", err)
	}

	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()

	client := &http.Client{Transport: transport, Timeout: 8 * time.Second}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/member", bytes.NewReader(data))
	if err != nil {
		return result, fmt.Errorf("construct member request: %w", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return result, fmt.Errorf("live member channel unavailable: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("member request rejected (HTTP %d)", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	if err != nil {
		return result, fmt.Errorf("read member receipt: %w", err)
	}

	if len(body) > 16<<10 {
		return result, errors.New("member receipt exceeds bound")
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("decode member result: %w", err)
	}

	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) || !operationPattern.MatchString(result.UserID) || !operationPattern.MatchString(result.LibraryID) {
		return MemberResult{}, errors.New("invalid member receipt")
	}

	if input.InvitationToken == "" && (result.OperationID != input.OperationID || result.TenantID != input.TenantID || result.Subject != input.Subject || result.Issuer != input.Issuer || result.BootstrapID != input.BootstrapID || result.LibraryID != input.LibraryID) {
		return MemberResult{}, errors.New("unbound member receipt")
	}

	return result, nil
}
