package sqlite

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestMigrationIdentitiesBindEveryEmbeddedSQLFile(t *testing.T) {
	identities, err := MigrationIdentities()
	if err != nil {
		t.Fatal(err)
	}

	version, err := CurrentSchemaVersion()
	if err != nil || len(identities) != version {
		t.Fatal(len(identities), version, err)
	}

	for index, identity := range identities {
		name := fmt.Sprintf("migrations/%04d_%s.sql", identity.Version, identity.Name)

		data, err := migrationFiles.ReadFile(name)
		if err != nil || identity.Version != index+1 || identity.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
			t.Fatal("migration identity differs from embedded source", identity, err)
		}
	}

	identities[0].SHA256 = "tampered"

	again, err := MigrationIdentities()
	if err != nil || again[0].SHA256 == "tampered" {
		t.Fatal("caller changed embedded compatibility", err)
	}
}
