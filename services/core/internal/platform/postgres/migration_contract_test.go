package postgres

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/peergo/peergo/contracts/go/schemaversionv1"
)

// Keep the cross-service startup gate synchronized with the migration set.
// A stale value prevents every Core-dependent process from starting after a
// successful migration, which is safer than drift but should be caught before
// deployment.
func TestExpectedMigrationVersionMatchesLatestCoreMigration(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(filepath.Join("..", "..", "..", "..", "..", "db", "core", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	var latest int64
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		prefix, _, found := strings.Cut(entry.Name(), "_")
		if !found {
			continue
		}
		version, parseErr := strconv.ParseInt(prefix, 10, 64)
		if parseErr != nil {
			t.Fatalf("parse migration version from %q: %v", entry.Name(), parseErr)
		}
		if version > latest {
			latest = version
		}
	}
	if latest == 0 {
		t.Fatal("no Core migrations found")
	}
	if schemaversionv1.Core != latest {
		t.Fatalf("Core schema version = %d, latest migration = %d", schemaversionv1.Core, latest)
	}
}
