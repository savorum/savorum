package sqlite

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"strconv"

	sqliteDriver "modernc.org/sqlite"
)

func registerShuffleFunction() error {
	err := sqliteDriver.RegisterDeterministicScalarFunction("savorum_shuffle", 2, func(_ *sqliteDriver.FunctionContext, args []driver.Value) (driver.Value, error) {
		id, idOK := args[0].(string)

		seed, seedOK := args[1].(int64)
		if !idOK || !seedOK {
			return nil, fmt.Errorf("shuffle requires an entry identifier and integer seed")
		}

		hash := sha256.Sum256([]byte(strconv.FormatInt(seed, 10) + "\x00" + id))

		return int64(binary.BigEndian.Uint64(hash[:8]) >> 1), nil
	})
	if err != nil {
		return fmt.Errorf("register savorum_shuffle: %w", err)
	}

	return nil
}
