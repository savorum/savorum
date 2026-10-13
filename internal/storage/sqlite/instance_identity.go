package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// generateULID generates a 26-character Crockford Base32 ULID
// using 48-bit millisecond timestamp and 80-bit cryptographic randomness.
func generateULID(now time.Time) (string, error) {
	var raw [16]byte

	ms := uint64(now.UnixMilli())

	// 48-bit timestamp (big-endian)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], ms)
	copy(raw[0:6], ts[2:8])

	// 80-bit cryptographic entropy
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}

	// 16 bytes = 128 bits -> 26 5-bit chunks (with 2 zero padding bits at MSB)
	var ulid [26]byte

	ulid[0] = crockfordAlphabet[(raw[0]&224)>>5]
	ulid[1] = crockfordAlphabet[raw[0]&31]
	ulid[2] = crockfordAlphabet[(raw[1]&248)>>3]
	ulid[3] = crockfordAlphabet[((raw[1]&7)<<2)|((raw[2]&192)>>6)]
	ulid[4] = crockfordAlphabet[(raw[2]&62)>>1]
	ulid[5] = crockfordAlphabet[((raw[2]&1)<<4)|((raw[3]&240)>>4)]
	ulid[6] = crockfordAlphabet[((raw[3]&15)<<1)|((raw[4]&128)>>7)]
	ulid[7] = crockfordAlphabet[(raw[4]&124)>>2]
	ulid[8] = crockfordAlphabet[((raw[4]&3)<<3)|((raw[5]&224)>>5)]
	ulid[9] = crockfordAlphabet[raw[5]&31]
	ulid[10] = crockfordAlphabet[(raw[6]&248)>>3]
	ulid[11] = crockfordAlphabet[((raw[6]&7)<<2)|((raw[7]&192)>>6)]
	ulid[12] = crockfordAlphabet[(raw[7]&62)>>1]
	ulid[13] = crockfordAlphabet[((raw[7]&1)<<4)|((raw[8]&240)>>4)]
	ulid[14] = crockfordAlphabet[((raw[8]&15)<<1)|((raw[9]&128)>>7)]
	ulid[15] = crockfordAlphabet[(raw[9]&124)>>2]
	ulid[16] = crockfordAlphabet[((raw[9]&3)<<3)|((raw[10]&224)>>5)]
	ulid[17] = crockfordAlphabet[raw[10]&31]
	ulid[18] = crockfordAlphabet[(raw[11]&248)>>3]
	ulid[19] = crockfordAlphabet[((raw[11]&7)<<2)|((raw[12]&192)>>6)]
	ulid[20] = crockfordAlphabet[(raw[12]&62)>>1]
	ulid[21] = crockfordAlphabet[((raw[12]&1)<<4)|((raw[13]&240)>>4)]
	ulid[22] = crockfordAlphabet[((raw[13]&15)<<1)|((raw[14]&128)>>7)]
	ulid[23] = crockfordAlphabet[(raw[14]&124)>>2]
	ulid[24] = crockfordAlphabet[((raw[14]&3)<<3)|((raw[15]&224)>>5)]
	ulid[25] = crockfordAlphabet[raw[15]&31]

	return string(ulid[:]), nil
}

// InstanceID returns the persistent random instance ID for this installation.
// If not yet set, it generates a new ULID and saves it.
func (s *Store) InstanceID(ctx context.Context) (string, error) {
	var id string

	err := s.db.QueryRowContext(ctx, "SELECT instance_id FROM instance_identity WHERE singleton = 1").Scan(&id)
	if err == nil {
		return id, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read instance identity: %w", err)
	}

	newID, err := generateULID(time.Now().UTC())
	if err != nil {
		return "", fmt.Errorf("generate instance ID: %w", err)
	}

	createdAt := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO instance_identity (singleton, instance_id, created_at)
		VALUES (1, ?, ?)
		ON CONFLICT(singleton) DO NOTHING`,
		newID, createdAt,
	)
	if err != nil {
		return "", fmt.Errorf("persist instance identity: %w", err)
	}

	// Re-query in case another concurrent process won the insert
	err = s.db.QueryRowContext(ctx, "SELECT instance_id FROM instance_identity WHERE singleton = 1").Scan(&id)
	if err != nil {
		return "", fmt.Errorf("read confirmed instance identity: %w", err)
	}

	return id, nil
}
