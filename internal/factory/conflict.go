package factory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/kpenfound/shed/internal/bundle"
	"github.com/kpenfound/shed/internal/clause"
)

// ConflictsDir is the directory of the state directory holding, per unit,
// the conflict markers shed wrote into the files it gave sessions (S.vcs.12).
const ConflictsDir = "conflicts"

// markers guards the records of ConflictsDir.
var markers sync.Mutex

// markerLine matches a line of the conflict markers jj writes into a
// conflicted file: a run of at least seven of one marker character, alone
// or followed by a space.
var markerLine = regexp.MustCompile(`^(<{7,}|>{7,}|\|{7,}|={7,}|%{7,}|\\{7,}|\+{7,}|-{7,})( .*)?$`)

// markerRecord maps each file shed wrote conflict markers into to the
// distinct marker lines it wrote.
type markerRecord map[string][]string

func (f *Factory) markersPath(change string) string {
	return filepath.Join(f.State, ConflictsDir, change+".json")
}

func (f *Factory) loadMarkers(change string) (markerRecord, error) {
	data, err := os.ReadFile(f.markersPath(change))
	if errors.Is(err, fs.ErrNotExist) {
		return markerRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	rec := markerRecord{}
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("the conflict markers of unit %s: %w", change, err)
	}
	return rec, nil
}

// recordMarkers records the marker lines of every file jj stores as
// conflicted that shed wrote into dir, a session's directory of a unit's
// files (S.vcs.12). Files recorded before that no longer hold any of their
// marker lines are dropped from the record.
func (f *Factory) recordMarkers(ctx context.Context, change, dir string) error {
	conflicted, err := f.Repo.Conflicted(ctx, change)
	if err != nil {
		return err
	}
	markers.Lock()
	defer markers.Unlock()
	old, err := f.loadMarkers(change)
	if err != nil {
		return err
	}
	rec := markerRecord{}
	for name, lines := range old {
		if holds, err := holdsAny(filepath.Join(dir, filepath.FromSlash(name)), lines); err != nil {
			return err
		} else if holds {
			rec[name] = lines
		}
	}
	for _, name := range conflicted {
		src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		lines := rec[name]
		for _, line := range strings.Split(string(src), "\n") {
			if markerLine.MatchString(line) && !slices.Contains(lines, line) {
				lines = append(lines, line)
			}
		}
		if len(lines) > 0 {
			rec[name] = lines
		}
	}
	path := f.markersPath(change)
	if len(rec) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// holdsAny reports whether a file holds any of lines as a whole line.
func holdsAny(path string, lines []string) (bool, error) {
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(src), "\n") {
		if slices.Contains(lines, line) {
			return true, nil
		}
	}
	return false, nil
}

// unresolved lists, sorted, each file on a unit's change that holds an
// unresolved conflict (S.vcs.12): jj stores it as conflicted, or it still
// holds a line of the conflict markers shed wrote into it. It also returns
// the marker lines of each file by the record.
func (f *Factory) unresolved(ctx context.Context, change string) ([]string, markerRecord, error) {
	conflicted, err := f.Repo.Conflicted(ctx, change)
	if err != nil {
		return nil, nil, err
	}
	dir, err := f.Repo.Workspace(ctx, change)
	if err != nil {
		return nil, nil, err
	}
	markers.Lock()
	rec, err := f.loadMarkers(change)
	markers.Unlock()
	if err != nil {
		return nil, nil, err
	}
	files := slices.Clone(conflicted)
	for name, lines := range rec {
		if slices.Contains(files, name) {
			continue
		}
		if holds, err := holdsAny(filepath.Join(dir, filepath.FromSlash(name)), lines); err != nil {
			return nil, nil, err
		} else if holds {
			files = append(files, name)
		}
	}
	slices.Sort(files)
	return files, rec, nil
}

// specConflicted names each file under spec/ on a unit's change that holds
// an unresolved conflict (S.vcs.12) and each clause ID inside a conflicted
// region, or returns "" when no file under spec/ holds one.
func (f *Factory) specConflicted(ctx context.Context, change string) (string, error) {
	files, rec, err := f.unresolved(ctx, change)
	if err != nil {
		return "", err
	}
	dir, err := f.Repo.Workspace(ctx, change)
	if err != nil {
		return "", err
	}
	var found []string
	for _, name := range files {
		if !strings.HasPrefix(name, "spec/") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		var clauses []string
		for _, id := range clause.Mentioned(conflictedRegions(string(src), rec[name])) {
			clauses = append(clauses, id.String())
		}
		entry := name
		if len(clauses) > 0 {
			entry += " (clauses " + strings.Join(clauses, ", ") + ")"
		}
		found = append(found, entry)
	}
	return strings.Join(found, "; "), nil
}

// conflictedRegions returns the text of a file from its first marker line
// to its last: lines jj writes as markers, or the marker lines recorded for
// the file.
func conflictedRegions(src string, recorded []string) string {
	lines := strings.Split(src, "\n")
	first, last := -1, -1
	for i, line := range lines {
		if markerLine.MatchString(line) || slices.Contains(recorded, line) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return ""
	}
	return strings.Join(lines[first:last+1], "\n")
}

// conflictSection tells a unit's mechanic of each file on its change that
// holds an unresolved conflict (S.vcs.10, S.vcs.12), or returns nil when
// none does.
func (f *Factory) conflictSection(ctx context.Context, change string) ([]bundle.Section, error) {
	files, _, err := f.unresolved(ctx, change)
	if err != nil || len(files) == 0 {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("These files hold unresolved conflicts with main. Before any other work, resolve each against the sealed spec, removing every conflict marker:\n\n")
	for _, name := range files {
		fmt.Fprintf(&b, "- %s\n", name)
	}
	return []bundle.Section{{Title: "Conflicts", Body: b.String()}}, nil
}
