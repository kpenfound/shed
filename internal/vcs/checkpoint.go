package vcs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// checkpointRecord is the operation-log entry the repository was at before
// a multi-step operation began.
type checkpointRecord struct {
	Operation string    `json:"operation"`
	Entry     string    `json:"entry"`
	Recorded  time.Time `json:"recorded"`
}

// checkpoint durably records the current operation-log entry before a
// multi-step operation. The returned function settles it: on success it
// drops the record; on failure it restores the repository to the entry, so a
// half-done operation leaves nothing behind. A crash leaves the record for
// the next Open to restore.
func (r *Repo) checkpoint(ctx context.Context, operation string) (func(error) error, error) {
	entry, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "operation", "log",
		"--no-graph", "--limit", "1", "-T", `self.id() ++ "\n"`)
	if err != nil {
		return nil, err
	}
	rec := checkpointRecord{Operation: operation, Entry: entry, Recorded: time.Now().UTC()}
	data, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(r.state, CheckpointsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, nonce()+".json")
	if err := writeDurably(path, data); err != nil {
		return nil, err
	}
	return func(opErr error) error {
		if opErr != nil {
			if err := r.restore(ctx, entry); err != nil {
				return errors.Join(opErr, fmt.Errorf("restoring the repository after %s: %w", operation, err))
			}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Join(opErr, err)
		}
		return opErr
	}, nil
}

// recover restores the repository to the oldest checkpoint a crash left
// behind, drops every checkpoint and returns the operations they were taken
// for, oldest first.
func (r *Repo) recover(ctx context.Context) ([]string, error) {
	dir := filepath.Join(r.state, CheckpointsDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []checkpointRecord
	var paths []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var rec checkpointRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("checkpoint %s: %w", e.Name(), err)
		}
		found = append(found, rec)
		paths = append(paths, path)
	}
	if len(found) == 0 {
		return nil, nil
	}
	oldest := slices.MinFunc(found, func(a, b checkpointRecord) int { return a.Recorded.Compare(b.Recorded) })
	if err := r.restore(ctx, oldest.Entry); err != nil {
		return nil, err
	}
	var operations []string
	slices.SortFunc(found, func(a, b checkpointRecord) int { return a.Recorded.Compare(b.Recorded) })
	for _, rec := range found {
		operations = append(operations, rec.Operation)
	}
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			return nil, err
		}
	}
	return operations, nil
}

// restore takes the repository back to an operation-log entry. Unit
// workspaces whose working copy the restore moved are updated, and the
// directories of workspaces the restore removed are deleted. The owner's
// working copy is not touched.
func (r *Repo) restore(ctx context.Context, entry string) error {
	if _, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "operation", "restore", entry); err != nil {
		return err
	}
	all, err := r.workspaces(ctx)
	if err != nil {
		return err
	}
	keep := map[string]bool{baseDir: true}
	dirs := []string{filepath.Join(r.state, WorkspacesDir, baseDir)}
	for _, w := range all {
		if _, err := os.Stat(w.dir); errors.Is(err, os.ErrNotExist) {
			// The restore brought back a workspace whose directory is gone.
			if _, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "workspace", "forget", w.name); err != nil {
				return err
			}
			continue
		}
		keep[w.name] = true
		dirs = append(dirs, w.dir)
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if _, err := r.run(ctx, dir, shedIdentity, "workspace", "update-stale"); err != nil &&
			!strings.Contains(err.Error(), "not stale") {
			return err
		}
	}
	entries, err := os.ReadDir(filepath.Join(r.state, WorkspacesDir))
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && !keep[e.Name()] {
			if err := os.RemoveAll(filepath.Join(r.state, WorkspacesDir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeDurably(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
