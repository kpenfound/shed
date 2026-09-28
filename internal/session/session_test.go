package session

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/kpenfound/busybees/core/agent"
	"github.com/kpenfound/busybees/core/mcphost"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kpenfound/shed/internal/config"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

var ctx = context.Background()

const change = "qpvuntsmwlqtqpvuntsmwlqt"

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func openTracker(t *testing.T) *tracker.Tracker {
	t.Helper()
	tr, err := tracker.Open(t.TempDir(), tracker.Options{})
	must(t, err)
	t.Cleanup(func() { tr.Close() })
	must(t, tr.OpenUnit(change, "Say goodbye", unit.Painter))
	return tr
}

type runnerFunc func(context.Context, Turn) (Result, error)

func (f runnerFunc) Run(ctx context.Context, t Turn) (Result, error) { return f(ctx, t) }

//shed:proves S.sess.5 S.sess.6 S.sess.9
func TestSessionsRecordEveryAttempt(t *testing.T) {
	tr := openTracker(t)
	notice, err := tr.AddNotice(change, unit.Painter, "seal-moved", "The seal moved.", unit.Wheelbuilder)
	must(t, err)
	var seen []Turn
	runner := runnerFunc(func(_ context.Context, turn Turn) (Result, error) {
		seen = append(seen, turn)
		if bundle, err := os.ReadFile(filepath.Join(turn.SessionDir, "bundle.md")); err != nil || string(bundle) != "the bundle" {
			t.Errorf("attempt %d bundle = %q, %v", turn.Attempt, bundle, err)
		}
		if pending, _ := tr.Notices(unit.Painter, true); len(pending) != 0 {
			t.Errorf("notices still pending during the session: %+v", pending)
		}
		if turn.Attempt == 0 {
			return Result{Failure: Infrastructure, Reason: "rate limited", CostUSD: 0.25}, nil
		}
		return Result{Status: "proposed", Note: "a small one", CostUSD: 1}, nil
	})
	s := &Sessions{Tracker: tr, Runner: runner, Retries: 2}
	res, err := s.Run(ctx, Turn{Unit: change, Role: unit.Painter, Step: "propose", Bundle: "the bundle",
		Notices: []string{notice}, StepDone: "proposed"})
	must(t, err)
	if res.Status != "proposed" || len(seen) != 2 || seen[1].Attempt != 1 {
		t.Fatalf("result %+v after %d attempts", res, len(seen))
	}
	sessions, err := tr.Sessions(change)
	must(t, err)
	if len(sessions) != 2 || sessions[0].Status != tracker.Failed || sessions[1].Status != tracker.Succeeded ||
		sessions[0].Outcome != "infrastructure: rate limited" || sessions[1].Outcome != "proposed: a small one" {
		t.Errorf("sessions = %+v", sessions)
	}
	if u, _ := tr.Unit(change); u.CostUSD != 1.25 || !slices.Equal(u.Steps, []string{"propose"}) {
		t.Errorf("unit = %+v", u)
	}

	// A session that reports nothing is a behavioural failure, not retried.
	calls := 0
	s.Runner = runnerFunc(func(context.Context, Turn) (Result, error) { calls++; return Result{}, nil })
	res, err = s.Run(ctx, Turn{Unit: change, Role: unit.Painter, Step: "propose"})
	must(t, err)
	if res.Failure != Behavioural || calls != 1 {
		t.Errorf("silent session = %+v after %d calls", res, calls)
	}
}

func testCore(t *testing.T, exec func(context.Context, agent.Request) (*agent.Result, error)) *Core {
	t.Helper()
	op := config.Defaults()
	op.Budget.PerSessionUSD = 2
	op.Profiles["backup"] = config.Profile{Agent: "codex", Model: "small", Mounts: []string{t.TempDir() + ":rw"}, Env: []string{"GOFLAGS"}}
	op.Profiles["default"] = config.Profile{Agent: "claude", Model: "big", Fallback: "backup", Template: "shed-go"}
	return &Core{Operator: op, Exec: exec}
}

// callTool calls a tool of a session's MCP server as the agent in the
// sandbox would, reaching the host's loopback directly.
func callTool(ctx context.Context, req agent.Request, name string, args map[string]any) (string, error) {
	entry := req.Profile.MCP[serverName]
	session, err := mcp.NewClient(&mcp.Implementation{Name: "fake-agent"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   entry.URL,
		HTTPClient: &http.Client{Transport: mcphost.BearerTransport(req.Env[tokenEnv], nil)},
	}, nil)
	if err != nil {
		return "", err
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if res.IsError {
		return text, errors.New(text)
	}
	return text, nil
}

//shed:proves S.sess.1 S.sess.3 S.sess.4 S.vcs.2
func TestCoreRunsSandboxedSessions(t *testing.T) {
	var c *Core
	var echoed string
	dir, sessionDir := t.TempDir(), t.TempDir()
	c = testCore(t, func(ctx context.Context, req agent.Request) (*agent.Result, error) {
		if req.Profile.Sandbox != agent.SandboxSbx || req.Profile.Agent != "claude" || req.Profile.Model != "big" || req.Profile.SandboxImage != "shed-go" {
			t.Errorf("profile = %+v", req.Profile)
		}
		turn, err := c.Runner.Verify(req)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if turn.VCS || req.Grants.VCS {
			t.Error("the session was granted version control")
		}
		for _, name := range []string{"git", "jj"} {
			if !slices.Contains(turn.DeniedExecutables, name) {
				t.Errorf("%s is not denied: %v", name, turn.DeniedExecutables)
			}
		}
		binds := map[string]agent.Access{}
		for _, b := range turn.Binds {
			binds[b.Source] = b.Access
		}
		if len(binds) != 2 || binds[evalDir(t, dir)] != agent.ReadWrite || binds[evalDir(t, sessionDir)] != agent.ReadWrite {
			t.Errorf("binds = %+v", turn.Binds)
		}
		// The tools server is granted to this sandbox alone, by its port.
		entry := req.Profile.MCP[serverName]
		u, err := url.Parse(entry.URL)
		if err != nil || entry.Type != "http" || u.Hostname() != "127.0.0.1" || entry.BearerTokenEnv != tokenEnv || req.Env[tokenEnv] == "" {
			t.Errorf("MCP entry = %+v", entry)
		}
		port, _ := strconv.Atoi(u.Port())
		want := []agent.HostServer{{Name: serverName, Port: port}}
		if !slices.Equal(req.Grants.HostServers, want) || !slices.Equal(turn.HostServers, want) {
			t.Errorf("host servers granted %+v, verified %+v; want %+v", req.Grants.HostServers, turn.HostServers, want)
		}
		if echoed, err = callTool(ctx, req, "echo", map[string]any{"text": "hi"}); err != nil {
			t.Errorf("echo: %v", err)
		}
		if _, err := callTool(ctx, req, "done", map[string]any{"status": "bogus"}); err == nil {
			t.Error("done accepted an outcome the turn does not allow")
		}
		if _, err := callTool(ctx, req, "done", map[string]any{"status": "done", "note": "all good"}); err != nil {
			t.Errorf("done: %v", err)
		}
		return &agent.Result{CostUSD: 0.75}, nil
	})
	type echoIn struct {
		Text string `json:"text"`
	}
	echo := NewTool("echo", "Echo text.", func(_ context.Context, in echoIn) (string, error) { return "echo: " + in.Text, nil })
	turn := Turn{Unit: change, Role: unit.Mechanic, Step: "implement", Dir: dir, Writable: true,
		SessionDir: sessionDir, Prompt: "work", Tools: []Tool{echo}, Outcomes: []string{"done", "reopen"}}
	res, err := c.Run(ctx, turn)
	must(t, err)
	if res.Status != "done" || res.Note != "all good" || res.CostUSD != 0.75 || res.Failure != NoFailure {
		t.Errorf("result = %+v", res)
	}
	if echoed != "echo: hi" {
		t.Errorf("echo returned %q", echoed)
	}

	// A reviewing role's working directory is read-write too: sbx needs its
	// primary workspace writable, and nothing captures it.
	c.Exec = func(ctx context.Context, req agent.Request) (*agent.Result, error) {
		v, err := c.Runner.Verify(req)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range v.Binds {
			if b.Source == evalDir(t, dir) && b.Access != agent.ReadWrite {
				t.Errorf("a reviewer's working directory is %s", b.Access)
			}
		}
		_, err = callTool(ctx, req, "done", map[string]any{"status": "pass"})
		return &agent.Result{}, err
	}
	turn.Role, turn.Writable, turn.Outcomes, turn.Tools = unit.Committee, false, []string{"pass", "fail"}, nil
	res, err = c.Run(ctx, turn)
	must(t, err)
	if res.Status != "pass" {
		t.Errorf("review result = %+v", res)
	}
}

func evalDir(t *testing.T, dir string) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(dir)
	must(t, err)
	return d
}

//shed:proves S.sess.3
func TestGrants(t *testing.T) {
	c := testCore(t, nil)
	profile, err := c.Profile("mechanic")
	must(t, err)
	dir, sessionDir := t.TempDir(), t.TempDir()
	g, err := c.Grants(Turn{Dir: dir, SessionDir: sessionDir, Writable: true}, profile)
	must(t, err)
	want := []agent.Mount{{Path: dir, Access: agent.ReadWrite}, {Path: sessionDir, Access: agent.ReadWrite}}
	if !slices.Equal(g.Mounts, want) {
		t.Errorf("mounts = %+v, want %+v", g.Mounts, want)
	}
	for _, name := range g.Env {
		if name == "PATH" || name == "HOME" || strings.HasPrefix(name, "GIT") || strings.HasPrefix(name, "SSH") {
			t.Errorf("env grants %s", name)
		}
	}
	if !slices.Contains(g.Env, tokenEnv) || g.VCS {
		t.Errorf("grants = %+v", g)
	}

	backup := *profile.Fallback
	g, err = c.Grants(Turn{Dir: dir, SessionDir: sessionDir}, backup)
	must(t, err)
	extra := c.Operator.Profiles["backup"].Mounts[0]
	if len(g.Mounts) != 3 || g.Mounts[2] != (agent.Mount{Path: strings.TrimSuffix(extra, ":rw"), Access: agent.ReadWrite}) ||
		g.Mounts[0].Access != agent.ReadWrite || !slices.Contains(g.Env, "GOFLAGS") {
		t.Errorf("fallback grants = %+v", g)
	}
}

//shed:proves S.sess.8 S.sess.9
func TestCoreFailures(t *testing.T) {
	var profiles []string
	var result *agent.Result
	c := testCore(t, func(_ context.Context, req agent.Request) (*agent.Result, error) {
		profiles = append(profiles, req.Profile.Name)
		if req.CostCapUSD != 2 {
			t.Errorf("cost cap = %v", req.CostCapUSD)
		}
		return result, nil
	})
	turn := Turn{Unit: change, Role: unit.Mechanic, Dir: t.TempDir(), SessionDir: t.TempDir(), Outcomes: []string{"done"}}

	result = &agent.Result{CostUSD: 2, CostCapped: true, ErrorSubtype: agent.SubtypeCostCap}
	res, err := c.Run(ctx, turn)
	must(t, err)
	if res.Failure != Infrastructure || !strings.Contains(res.Reason, "cost cap of $2.00") {
		t.Errorf("capped session = %+v", res)
	}

	result = &agent.Result{ExitCode: 1, IsError: true, ErrorSubtype: "error_during_execution"}
	turn.Attempt = 1
	res, err = c.Run(ctx, turn)
	must(t, err)
	if res.Failure == NoFailure {
		t.Errorf("failed session = %+v", res)
	}
	if !slices.Equal(profiles, []string{"default", "backup"}) {
		t.Errorf("profiles by attempt = %v", profiles)
	}
}
