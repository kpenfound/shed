// Package roles holds the prompts shed's roles run with. Each prompt ships
// with shed; an operator may replace one by writing a file of the same name
// under prompts/ in the state directory.
package roles

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed prompts/*.md
var prompts embed.FS

// Prompt names.
const (
	Painter         = "painter"
	PainterReply    = "painter-reply"
	Committee       = "committee"
	CommitteeReview = "committee-review"
	Mechanic        = "mechanic"
	Wheelbuilder    = "wheelbuilder"
	FrameBuilder    = "frame-builder"
	// WheelbuilderReview reviews a unit against changed horizon clauses.
	WheelbuilderReview = "wheelbuilder-review"
	common             = "common"
)

// Names lists every prompt a role runs with.
var Names = []string{Painter, PainterReply, Committee, CommitteeReview, Mechanic, Wheelbuilder, WheelbuilderReview, FrameBuilder}

// OverrideDir is the directory under the state directory whose files
// replace the shipped prompts.
const OverrideDir = "prompts"

// System returns a role's system prompt: what every role shares, then the
// role's own prompt. A file in the state directory's prompts/ replaces the
// shipped one of the same name.
func System(stateDir, name string) (string, error) {
	shared, err := load(stateDir, common)
	if err != nil {
		return "", err
	}
	own, err := load(stateDir, name)
	if err != nil {
		return "", err
	}
	return shared + "\n" + own, nil
}

func load(stateDir, name string) (string, error) {
	if stateDir != "" {
		data, err := os.ReadFile(filepath.Join(stateDir, OverrideDir, name+".md"))
		if err == nil {
			return string(data), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	data, err := prompts.ReadFile("prompts/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("no prompt %q", name)
	}
	return string(data), nil
}
