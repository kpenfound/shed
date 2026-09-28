package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/kpenfound/busybees/core/agent"
	"github.com/kpenfound/busybees/core/mcphost"
	"github.com/kpenfound/busybees/core/ops"
	corevcs "github.com/kpenfound/busybees/core/vcs"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kpenfound/shed/internal/config"
)

// tokenEnv carries the bearer token of the session's MCP endpoint.
const tokenEnv = "SHED_MCP_TOKEN"

// serverName is the MCP server a session reaches shed's tools on.
const serverName = "shed"

// baseEnv are the host variables every session inherits. A sandbox has its
// own PATH and HOME, so neither is passed in.
var baseEnv = []string{"LANG", "LC_*", "TERM", "TZ", tokenEnv}

// Core runs sessions through busybees/core, each in a Docker Sandbox: a
// microVM that sees only the directories the session is granted and holds
// the agent's credential in its own proxy. A session is never granted
// version control.
type Core struct {
	Operator config.Operator
	// Runner runs the agent; its sbx binary and sessions directory are set
	// by the caller.
	Runner agent.Runner
	// Exec runs a verified request. Nil runs it with Runner.
	Exec func(ctx context.Context, req agent.Request) (*agent.Result, error)
	// StartMCP serves a session's tools. Nil serves them on a fresh
	// loopback port, which the session's sandbox alone is allowed to reach.
	StartMCP mcphost.StartFunc
}

// Profile returns the core profile a role's sessions run on, with its
// fallback chain.
func (c *Core) Profile(role string) (agent.Profile, error) {
	r, ok := c.Operator.Roles[role]
	if !ok {
		return agent.Profile{}, fmt.Errorf("no role %q in the operator settings", role)
	}
	return c.profile(r.Profile, map[string]bool{})
}

func (c *Core) profile(name string, seen map[string]bool) (agent.Profile, error) {
	p, ok := c.Operator.Profiles[name]
	if !ok {
		return agent.Profile{}, fmt.Errorf("no profile %q in the operator settings", name)
	}
	if seen[name] {
		return agent.Profile{}, fmt.Errorf("profile %q falls back to itself", name)
	}
	seen[name] = true
	out := agent.Profile{Name: name, Agent: p.Agent, Model: p.Model, Effort: p.Effort, MaxTurns: p.MaxTurns,
		Timeout: p.Timeout.Duration, Sandbox: agent.SandboxSbx, SandboxImage: p.Template}
	if p.Fallback != "" {
		fb, err := c.profile(p.Fallback, seen)
		if err != nil {
			return agent.Profile{}, err
		}
		out.Fallback = &fb
	}
	return out, nil
}

// Grants returns what a session of a turn may use: its working directory,
// its session directory and the profile's extra mounts. It never includes
// version control. The working directory is read-write for every role: sbx
// cannot start a sandbox whose primary workspace is read-only, and the
// directory is a copy of the unit's files made for this session alone, so
// what a reviewing role writes there is thrown away uncaptured.
func (c *Core) Grants(t Turn, p agent.Profile) (agent.Grants, error) {
	g := agent.Grants{
		Env:   slices.Clone(baseEnv),
		Tools: []string{agent.ToolsAll, "mcp__" + serverName},
		Mounts: []agent.Mount{
			{Path: t.Dir, Access: agent.ReadWrite},
			{Path: t.SessionDir, Access: agent.ReadWrite},
		},
	}
	prof := c.Operator.Profiles[p.Name]
	for _, m := range prof.Mounts {
		path, writable, err := config.ParseMount(m)
		if err != nil {
			return agent.Grants{}, err
		}
		if rest, ok := strings.CutPrefix(path, "~"); ok {
			home, err := os.UserHomeDir()
			if err != nil {
				return agent.Grants{}, err
			}
			path = filepath.Join(home, rest)
		}
		access := agent.ReadOnly
		if writable {
			access = agent.ReadWrite
		}
		g.Mounts = append(g.Mounts, agent.Mount{Path: path, Access: access})
	}
	g.Env = append(g.Env, prof.Env...)
	slices.Sort(g.Env)
	g.Env = slices.Compact(g.Env)
	return g, nil
}

// Run runs one attempt of a turn. The attempt selects the profile along the
// role's fallback chain.
func (c *Core) Run(ctx context.Context, t Turn) (Result, error) {
	base, err := c.Profile(string(t.Role))
	if err != nil {
		return Result{}, err
	}
	profile, _ := ops.SelectProfile(base, t.Attempt)

	registry := mcphost.NewRegistry([]string{string(t.Role)}, mcphost.RejectRole, mcphost.RejectRole)
	for _, tool := range t.Tools {
		tool.register(registry, string(t.Role))
	}
	var mu sync.Mutex
	var reported agent.Outcome
	var hasOutcome bool
	mcphost.AddDone(registry, mcphost.DoneOptions{
		Title:       "Report the outcome",
		Description: "Call this once, when the task is finished, with the outcome.",
		Statuses:    t.Outcomes,
		Report: func(_ context.Context, o agent.Outcome) (agent.Outcome, error) {
			if err := agent.ValidateOutcome(string(t.Role), o.Status, t.Outcomes); err != nil {
				return o, err
			}
			mu.Lock()
			reported, hasOutcome = o, true
			mu.Unlock()
			return o, nil
		},
	}, string(t.Role))
	server, err := registry.NewServer(string(t.Role), mcp.Implementation{Name: serverName, Version: "1"})
	if err != nil {
		return Result{}, err
	}
	start := c.StartMCP
	if start == nil {
		start = mcphost.Start
	}
	endpoint, lease, err := start(ctx, server)
	if err != nil {
		return Result{}, err
	}
	defer lease.Close()

	// The entry names the server as this host sees it. Granting it as a
	// host server lets this session's sandbox, and no other, reach its port;
	// core gives the session the entry at hostAlias.
	port, err := strconv.Atoi(endpoint.Port)
	if err != nil {
		return Result{}, fmt.Errorf("the tools server's port %q: %w", endpoint.Port, err)
	}
	profile.MCP = map[string]agent.MCPEntry{serverName: {Type: "http", URL: endpoint.URL, BearerTokenEnv: tokenEnv}}
	grants, err := c.Grants(t, profile)
	if err != nil {
		return Result{}, err
	}
	grants.HostServers = []agent.HostServer{{Name: serverName, Port: port}}
	req := agent.Request{
		Name:          fmt.Sprintf("shed-%s-%s", t.Role, filepath.Base(t.SessionDir)),
		Profile:       profile,
		ValidOutcomes: t.Outcomes,
		Workspace:     corevcs.Directory(t.Dir),
		SystemPrompt:  t.SystemPrompt,
		Prompt:        t.Prompt,
		Env:           map[string]string{tokenEnv: endpoint.Token},
		SessionDir:    t.SessionDir,
		Grants:        &grants,
		ResumeID:      t.ResumeID,
		CostCapUSD:    c.Operator.Budget.PerSessionUSD,
	}

	if _, err := c.Runner.Verify(req); err != nil {
		return Result{}, fmt.Errorf("the session's grants: %w", err)
	}
	exec := c.Exec
	if exec == nil {
		exec = c.Runner.Run
	}
	res, err := exec(ctx, req)
	if err != nil {
		return Result{}, err
	}

	out := Result{CostUSD: res.CostUSD, ResumeID: res.ClaudeID}
	mu.Lock()
	if !hasOutcome && res.HasOutcome {
		reported, hasOutcome = res.Outcome, true
	}
	mu.Unlock()
	switch {
	case res.CostCapped:
		out.Failure, out.Reason = Infrastructure, fmt.Sprintf("the session reached its cost cap of $%.2f", req.CostCapUSD)
	case hasOutcome:
		out.Status, out.Note = reported.Status, reported.Note
	default:
		switch ops.ClassifyFailure(res) {
		case ops.FailureInfra:
			out.Failure, out.Reason = Infrastructure, ops.InfraReason(res)
		default:
			out.Failure, out.Reason = Behavioural, "the session ended without reporting an outcome"
		}
	}
	if out.Reason == "" && out.Failure != NoFailure {
		out.Reason = "unknown"
	}
	return out, nil
}
