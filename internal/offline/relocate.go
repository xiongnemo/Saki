package offline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiongnemo/saki/internal/paths"
)

// RewriteDownloadRoot points the downloads recorded in the catalog database at
// dbPath from oldRoot to newRoot, after `saki migrate` moved the files. Only
// paths below oldRoot whose file now exists below newRoot change, so a file
// that could not be moved keeps its old path. It returns how many rows
// changed.
func RewriteDownloadRoot(ctx context.Context, dbPath, oldRoot, newRoot, goos string) (int, error) {
	db, err := openDB(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, "SELECT song_id, variant, path FROM downloads WHERE path != ''")
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, err
	}
	type change struct{ songID, variant, path string }
	var changes []change
	for rows.Next() {
		var songID, variant, path string
		if err := rows.Scan(&songID, &variant, &path); err != nil {
			_ = rows.Close()
			return 0, err
		}
		rest, ok := paths.Within(goos, oldRoot, path)
		if !ok {
			continue
		}
		moved := filepath.Join(newRoot, rest)
		if _, err := os.Stat(moved); err != nil {
			continue
		}
		changes = append(changes, change{songID, variant, moved})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, c := range changes {
		if _, err := tx.ExecContext(ctx, "UPDATE downloads SET path=? WHERE song_id=? AND variant=?", c.path, c.songID, c.variant); err != nil {
			return 0, fmt.Errorf("update download %s/%s: %w", c.songID, c.variant, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(changes), nil
}
