package roles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

//shed:proves S.sess.2
func TestSystemPrompts(t *testing.T) {
	state := t.TempDir()
	for _, name := range Names {
		p, err := System(state, name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(p, "spec-driven software factory") || !strings.Contains(p, "`done`") {
			t.Errorf("%s prompt lacks the shared part", name)
		}
		if len(p) < 600 {
			t.Errorf("%s prompt is %d bytes", name, len(p))
		}
	}
	mechanic, _ := System(state, Mechanic)
	painter, _ := System(state, Painter)
	if mechanic == painter {
		t.Error("roles share one prompt")
	}

	if err := os.MkdirAll(filepath.Join(state, OverrideDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, OverrideDir, Mechanic+".md"), []byte("Custom mechanic."), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := System(state, Mechanic)
	if err != nil || !strings.HasSuffix(p, "Custom mechanic.") || !strings.Contains(p, "spec-driven software factory") {
		t.Errorf("overridden prompt = %q, %v", p, err)
	}
	if _, err := System(state, "nobody"); err == nil {
		t.Error("a prompt for an unknown role")
	}
}
