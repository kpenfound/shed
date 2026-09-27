// Package config reads shed's two settings files: shed.toml, the project
// settings committed at the repository root, and the operator settings in the
// state directory.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// File is the project settings file, relative to the repository root.
const File = "shed.toml"

// Project is the content of shed.toml.
type Project struct {
	Proofs Proofs `toml:"proofs"`
}

// Proofs configures how proofs run.
type Proofs struct {
	// Runner is a command shed puts in front of the go test command line.
	// A relative path is resolved from the repository root.
	Runner []string `toml:"runner"`
}

// LoadProject reads shed.toml from root. A missing file gives the defaults.
func LoadProject(root string) (Project, error) {
	var c Project
	md, err := toml.DecodeFile(filepath.Join(root, File), &c)
	if errors.Is(err, fs.ErrNotExist) {
		return Project{}, nil
	}
	if err != nil {
		return Project{}, fmt.Errorf("%s: %w", File, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Project{}, fmt.Errorf("%s: unknown setting %s", File, undecoded[0])
	}
	return c, nil
}
