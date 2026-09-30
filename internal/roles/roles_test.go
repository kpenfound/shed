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

//shed:proves S.fp.1 S.shed.20 S.sess.2
func TestCommitteePerspectivesAndDependencyBoundary(t *testing.T) {
	shared, err := System("", Committee)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"direct behavioral guarantees", "not recursively required", "concrete behavior", "first round", "Prior acceptance"} {
		if !strings.Contains(shared, text) {
			t.Errorf("committee instruction missing %q", text)
		}
	}
	seen := map[string]bool{}
	for _, name := range []string{"correctness", "integration", "scope"} {
		focus, err := Perspective("", name)
		if err != nil {
			t.Fatal(err)
		}
		if seen[focus] || !strings.Contains(strings.Join(strings.Fields(focus), " "), "any valid objection") {
			t.Errorf("invalid perspective %s", name)
		}
		seen[focus] = true
	}
	state := t.TempDir()
	if err := os.MkdirAll(filepath.Join(state, OverrideDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, OverrideDir, "committee-scope.md"), []byte("Custom focus"), 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := Perspective(state, "scope"); err != nil || got != "Custom focus" {
		t.Fatalf("override %q %v", got, err)
	}
}
