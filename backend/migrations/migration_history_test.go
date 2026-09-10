package migrations

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// This fixture freezes the Personal baseline, not the latest migration set.
// Add new migrations normally; never regenerate it to accept a history rewrite.
// It is embedded only in the test binary, not in the server's migration FS.
//
//go:embed testdata/personal_0_2_1_p1_migrations.json
var personalMigrationHistoryJSON []byte

type migrationHistoryEntry struct {
	Filename     string `json:"filename"`
	SHA256       string `json:"sha256"`
	Checksum     string `json:"checksum"`
	PersonalOnly bool   `json:"personal_only"`
}

func migrationHistoryDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func checkHistoricalMigration(fsys fs.FS, entry migrationHistoryEntry) error {
	content, err := fs.ReadFile(fsys, entry.Filename)
	if err != nil {
		return fmt.Errorf("read historical migration %s: %w", entry.Filename, err)
	}
	// Match repository.applyMigrationsFS exactly: trim surrounding whitespace,
	// but do not normalize line endings inside the SQL before hashing.
	checksum := migrationHistoryDigest([]byte(strings.TrimSpace(string(content))))
	if checksum != entry.Checksum {
		return fmt.Errorf("historical migration %s runner checksum changed: got %s, want %s", entry.Filename, checksum, entry.Checksum)
	}
	if actual := migrationHistoryDigest(content); actual != entry.SHA256 {
		return fmt.Errorf("historical migration %s raw bytes changed: got %s, want %s", entry.Filename, actual, entry.SHA256)
	}
	return nil
}

func TestPersonalMigrationHistory(t *testing.T) {
	var history struct {
		BaselineCommit string                  `json:"baseline_commit"`
		Migrations     []migrationHistoryEntry `json:"migrations"`
	}
	require.NoError(t, json.Unmarshal(personalMigrationHistoryJSON, &history))
	require.Equal(t, "9883b9447623ddec06ec7ee3a2e52823411f2dd2", history.BaselineCommit)
	require.Len(t, history.Migrations, 297, "the frozen baseline must not lose historical entries")

	previous := ""
	personalOnly := 0
	for _, entry := range history.Migrations {
		require.True(t, fs.ValidPath(entry.Filename) && !strings.Contains(entry.Filename, "/") && strings.HasSuffix(entry.Filename, ".sql"))
		require.Greater(t, entry.Filename, previous, "history must be unique and sorted by full filename, like the runner")
		previous = entry.Filename
		if entry.PersonalOnly {
			personalOnly++
		}
		for _, digest := range []string{entry.SHA256, entry.Checksum} {
			decoded, err := hex.DecodeString(digest)
			require.NoError(t, err)
			require.Len(t, decoded, sha256.Size)
		}
		t.Run(entry.Filename, func(t *testing.T) {
			require.NoError(t, checkHistoricalMigration(FS, entry), "restore the historical file; put intended changes in a new migration")
		})
	}
	require.Equal(t, 16, personalOnly, "the Personal-only migration set must remain protected")
}

func TestMigrationHistoryGuardRejectsDrift(t *testing.T) {
	original := []byte("CREATE TABLE history_guard (id BIGINT);\nSELECT 1;\n")
	entry := migrationHistoryEntry{
		Filename: "001_history_guard.sql",
		SHA256:   migrationHistoryDigest(original),
		Checksum: migrationHistoryDigest([]byte(strings.TrimSpace(string(original)))),
	}
	cases := []struct {
		name    string
		files   fstest.MapFS
		message string
	}{
		{"missing", fstest.MapFS{}, "read historical migration"},
		{"renamed", fstest.MapFS{"002_history_guard.sql": {Data: original}}, "read historical migration"},
		{"content", fstest.MapFS{entry.Filename: {Data: []byte("SELECT 2;\n")}}, "runner checksum changed"},
		{"line_endings", fstest.MapFS{entry.Filename: {Data: []byte(strings.ReplaceAll(string(original), "\n", "\r\n"))}}, "runner checksum changed"},
		{"surrounding_whitespace", fstest.MapFS{entry.Filename: {Data: append([]byte("\n"), original...)}}, "raw bytes changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkHistoricalMigration(tc.files, entry)
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestMigrationHistoryGuardAllowsAdditiveMigrations(t *testing.T) {
	original := []byte("SELECT 1;\n")
	entry := migrationHistoryEntry{
		Filename: "001_history_guard.sql",
		SHA256:   migrationHistoryDigest(original),
		Checksum: migrationHistoryDigest([]byte(strings.TrimSpace(string(original)))),
	}
	files := fstest.MapFS{
		entry.Filename:          {Data: original},
		"002_new_migration.sql": {Data: []byte("SELECT 2;\n")},
	}
	require.NoError(t, checkHistoricalMigration(files, entry))
}
