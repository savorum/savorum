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

// OperatorCallbacks bind private operations to the live guarded store.
type OperatorCallbacks struct {
	Member       MemberCallback
	CheckOwner   func(context.Context, MemberAuthority) error
	RevokeMember func(context.Context, MemberAuthority) error
	EraseUser    func(context.Context, Erasure) error
}

// OperationReceipt binds an acknowledged operation to its exact local identity.
type OperationReceipt struct {
	UserID      string `json:"user_id"`
	Outcome     string `json:"outcome"`
	TenantID    string `json:"tenant_id"`
	BootstrapID string `json:"bootstrap_id"`
}

// OperatorHandler is installed only on the private Unix listener.
func OperatorHandler(callbacks OperatorCallbacks) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/member", MemberHandler(callbacks.Member))
	mux.Handle("/check-owner", authorityHandler(callbacks.CheckOwner, "authorized"))
	mux.Handle("/revoke-member", authorityHandler(callbacks.RevokeMember, "revoked"))
	mux.Handle("/erase-user", operationHandler(func(ctx context.Context, e Erasure) (OperationReceipt, error) {
		err := callbacks.EraseUser(ctx, e)
		return OperationReceipt{UserID: e.UserID, Outcome: "erased", TenantID: e.TenantID, BootstrapID: e.BootstrapID}, err
	}))

	return mux
}

func authorityHandler(callback func(context.Context, MemberAuthority) error, outcome string) http.Handler {
	return operationHandler(func(ctx context.Context, m MemberAuthority) (OperationReceipt, error) {
		err := callback(ctx, m)
		return OperationReceipt{UserID: m.UserID, Outcome: outcome, TenantID: m.TenantID, BootstrapID: m.BootstrapID}, err
	})
}

func operationHandler[T interface{ Validate() error }](callback func(context.Context, T) (OperationReceipt, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		var input T

		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()

		var extra any
		if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(&extra), io.EOF) || input.Validate() != nil {
			http.Error(w, "Invalid operator request", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		receipt, err := callback(ctx, input)
		if err != nil {
			operatorError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(receipt)
	})
}

func operatorError(w http.ResponseWriter, err error) {
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
}

// RequestAuthority calls owner inspection or revocation in the serving process.
func RequestAuthority(ctx context.Context, socket string, input MemberAuthority, revoke bool) error {
	if err := input.Validate(); err != nil {
		return err
	}

	path, outcome := "/check-owner", "authorized"
	if revoke {
		path, outcome = "/revoke-member", "revoked"
	}

	return requestOperation(ctx, socket, path, input, OperationReceipt{UserID: input.UserID, Outcome: outcome, TenantID: input.TenantID, BootstrapID: input.BootstrapID})
}

// RequestErasure erases an identity through the serving process's admission.
func RequestErasure(ctx context.Context, socket string, input Erasure) error {
	if err := input.Validate(); err != nil {
		return err
	}

	return requestOperation(ctx, socket, "/erase-user", input, OperationReceipt{UserID: input.UserID, Outcome: "erased", TenantID: input.TenantID, BootstrapID: input.BootstrapID})
}

func requestOperation(ctx context.Context, socket, path string, input any, expected OperationReceipt) error {
	if !filepath.IsAbs(socket) {
		return errors.New("operator socket must be an absolute path")
	}

	data, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode operator request: %w", err)
	}

	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()

	client := &http.Client{Transport: transport, Timeout: 8 * time.Second}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost"+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("construct operator request: %w", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("live operator channel unavailable: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("operator request rejected (HTTP %d)", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
	if err != nil {
		return fmt.Errorf("read operator receipt: %w", err)
	}

	if len(body) > 16<<10 {
		return errors.New("operator receipt exceeds bound")
	}

	var receipt OperationReceipt

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()

	var extra any
	if decoder.Decode(&receipt) != nil || !errors.Is(decoder.Decode(&extra), io.EOF) || receipt != expected {
		return errors.New("invalid or unbound operator receipt")
	}

	return nil
}
