package identity

import "context"

// AuditEvent is deliberately limited to identifiers and operation metadata.
// Callers cannot attach arbitrary request bodies or credential material.
type AuditEvent struct {
	ID            int64  `json:"id"`
	CreatedAt     int64  `json:"created_at"`
	ActorID       string `json:"actor_id"`
	CredentialID  string `json:"credential_id"`
	Action        string `json:"action"`
	TargetType    string `json:"target_type"`
	TargetID      string `json:"target_id"`
	LibraryID     string `json:"library_id"`
	CorrelationID string `json:"correlation_id"`
	Outcome       string `json:"outcome"`
}

// AuditFilter bounds audit retrieval with a stable descending cursor.
type AuditFilter struct {
	LibraryID string
	ActorID   string
	Category  string
	TargetID  string
	Since     int64
	Until     int64
	Before    int64
}

type correlationKey struct{}

// WithCorrelation binds a server-generated request/job identifier.
func WithCorrelation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

// Correlation returns the trusted request/job identifier, if available.
func Correlation(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}
