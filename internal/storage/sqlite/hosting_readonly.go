package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/savorum/savorum/internal/hosting"
)

// HostingIdentity is the bootstrap identity exposed to a hosting capacity probe.
type HostingIdentity struct {
	TenantID   string
	InstanceID string
}

// ReadHostingIdentity opens an existing database read-only and reads only its
// hosted identity. It does not run storage policy preparation or migrations.
func ReadHostingIdentity(ctx context.Context, path string) (HostingIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return HostingIdentity{}, fmt.Errorf("inspect hosting database: %w", err)
	}

	if !info.Mode().IsRegular() {
		return HostingIdentity{}, errors.New("hosting database must be a regular file")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return HostingIdentity{}, fmt.Errorf("resolve hosting database: %w", err)
	}

	uri := &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	query := uri.Query()
	query.Set("mode", "ro")
	uri.RawQuery = query.Encode()

	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return HostingIdentity{}, fmt.Errorf("open hosting database read-only: %w", err)
	}
	defer database.Close()

	database.SetMaxOpenConns(1)

	var identity HostingIdentity

	err = database.QueryRowContext(ctx, `SELECT h.tenant_id, COALESCE(i.instance_id, '')
		FROM hosting_tenant h LEFT JOIN instance_identity i ON i.singleton=1 WHERE h.singleton=1`).
		Scan(&identity.TenantID, &identity.InstanceID)
	if errors.Is(err, sql.ErrNoRows) {
		return HostingIdentity{}, hosting.ErrNotHosted
	}

	if err != nil {
		return HostingIdentity{}, fmt.Errorf("read hosting identity: %w", err)
	}

	return identity, nil
}
