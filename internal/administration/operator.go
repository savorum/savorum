package administration

import (
	"context"
	"net/http"

	"github.com/savorum/savorum/internal/hosting"
	"github.com/savorum/savorum/internal/standby"
)

// OperatorCallbacks delegates identity decisions to the local instance.
type OperatorCallbacks = hosting.OperatorCallbacks

// OperatorHandler serves local authorization through the private operator socket.
func OperatorHandler(callbacks OperatorCallbacks) http.Handler {
	return hosting.OperatorHandler(callbacks)
}

// ChangeMaintenanceLease submits an absolute expiry or conditional release.
func ChangeMaintenanceLease(ctx context.Context, socket string, change standby.LeaseChange) error {
	return standby.ChangeMaintenanceLease(ctx, socket, change)
}

// RequestAuthority checks or revokes authority against local membership records.
func RequestAuthority(ctx context.Context, socket string, input hosting.MemberAuthority, revoke bool) error {
	return hosting.RequestAuthority(ctx, socket, input, revoke)
}

// RequestErasure preserves the local instance's ownership and administrator invariants.
func RequestErasure(ctx context.Context, socket string, input hosting.Erasure) error {
	return hosting.RequestErasure(ctx, socket, input)
}
