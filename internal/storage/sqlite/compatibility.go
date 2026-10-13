package sqlite

// MigrationIdentity names one immutable step of the database format.
type MigrationIdentity struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	SHA256  string `json:"sha256"`
}

// MigrationIdentities reports the verified embedded ledger without opening a database.
func MigrationIdentities() ([]MigrationIdentity, error) {
	migrations, err := loadMigrations()
	if err != nil {
		return nil, err
	}

	identities := make([]MigrationIdentity, len(migrations))
	for i, m := range migrations {
		identities[i] = MigrationIdentity{Version: m.version, Name: m.name, SHA256: m.checksum}
	}

	return identities, nil
}
