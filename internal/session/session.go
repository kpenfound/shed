// Package session runs agent sessions for shed's roles. Every session is
// ephemeral: it gets a plain directory of files, a bundle of context, the
// tools its role needs and a prompt, and it ends by reporting an outcome
// through its done tool. Shed records each session in the tracker and retries
// the ones that fail for infrastructure reasons.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/kpenfound/busybees/core/mcphost"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// Tool is a tool shed serves to a session. A fake agent in a test calls it
// directly; a real agent reaches it over MCP.
type Tool struct {
	Name        string
	Description string
	register    func(r *mcphost.Registry, role string)
	call        func(ctx context.Context, raw json.RawMessage) (string, error)
}

// NewTool defines a tool whose input decodes into In.
func NewTool[In any](name, description string, handle func(context.Context, In) (string, error)) Tool {
	return Tool{
		Name:        name,
		Description: description,
		register: func(r *mcphost.Registry, role string) {
			mcphost.AddTool(r, &mcp.Tool{Name: name, Description: description},
				func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
					text, err := handle(ctx, in)
					if err != nil {
						return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
					}
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
				}, role)
		},
		call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in In
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", fmt.Errorf("%s: %w", name, err)
				}
			}
			return handle(ctx, in)
		},
	}
}

// Call runs the tool with a JSON input, as an agent's call would.
func (t Tool) Call(ctx context.Context, input any) (string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return t.call(ctx, raw)
}

// Turn is one session to run.
type Turn struct {
	Unit string
	Role unit.Actor
	// Step is the formula step or shed stage the session works on.
	Step string
	// Dir is the session's working directory: a plain directory of files.
	Dir string
	// Writable marks a session whose files shed keeps: the caller captures
	// Dir onto the unit's change when it ends. What a reviewing role writes
	// is thrown away with Dir.
	Writable bool
	// SystemPrompt describes the role; Prompt is the task.
	SystemPrompt string
	Prompt       string
	// Bundle is the context shed assembled for the session. It is written
	// to the session directory before the session starts.
	Bundle string
	// Notices are the IDs of the notices the bundle carries. They count as
	// delivered once the session starts.
	Notices []string
	Tools   []Tool
	// Outcomes are the statuses the done tool accepts.
	Outcomes []string
	// Check, when set, vets each outcome before the done tool accepts it.
	Check func(status, note string) error
	// StepDone is the outcome that finishes Step.
	StepDone string
	// ResumeID continues an earlier session of the same role on the unit.
	ResumeID string

	// SessionDir and Attempt are set by Sessions.
	SessionDir string
	Attempt    int
}

// Failure is why a session did not produce an outcome.
type Failure string

const (
	// NoFailure is a session that reported an outcome.
	NoFailure Failure = ""
	// Infrastructure failures are worth retrying: the agent could not run,
	// hit a limit, or timed out.
	Infrastructure Failure = "infrastructure"
	// Behavioural failures are the agent's own: it ran and did not report.
	Behavioural Failure = "behavioural"
)

// Result is how a session ended.
type Result struct {
	Status   string
	Note     string
	CostUSD  float64
	Failure  Failure
	Reason   string
	ResumeID string
	// Session is the tracker's ID of the last attempt.
	Session string
}

// Runner runs one attempt of a turn.
type Runner interface {
	Run(ctx context.Context, t Turn) (Result, error)
}

// Sessions runs turns and records them in the tracker.
type Sessions struct {
	Tracker *tracker.Tracker
	Runner  Runner
	// Retries is how many more attempts an infrastructure failure gets.
	Retries int
}

// Run runs a turn, retrying infrastructure failures. Each attempt is its
// own session in the tracker, with the bundle written to its directory. The
// attempt's step counts as finished when it reports t.StepDone.
func (s *Sessions) Run(ctx context.Context, t Turn) (Result, error) {
	if s.Tracker == nil || s.Runner == nil {
		return Result{}, errors.New("sessions need a tracker and a runner")
	}
	for _, id := range t.Notices {
		if err := s.Tracker.DeliverNotice(id); err != nil {
			return Result{}, err
		}
	}
	for attempt := 0; ; attempt++ {
		rec, err := s.Tracker.StartSession(t.Unit, t.Role, t.Step, os.Getpid())
		if err != nil {
			return Result{}, err
		}
		if err := os.WriteFile(rec.BundlePath(), []byte(t.Bundle), 0o644); err != nil {
			return Result{}, errors.Join(err, s.Tracker.FinishSession(rec.ID, tracker.Failed, "", 0, false))
		}
		t.SessionDir, t.Attempt = rec.Dir, attempt
		res, err := s.Runner.Run(ctx, t)
		if err != nil {
			return Result{}, errors.Join(err, s.Tracker.FinishSession(rec.ID, tracker.Failed, err.Error(), res.CostUSD, false))
		}
		if res.Failure == NoFailure && res.Status == "" {
			res.Failure, res.Reason = Behavioural, "the session ended without reporting an outcome"
		}
		status, outcome := tracker.Succeeded, res.Status
		if res.Failure != NoFailure {
			status, outcome = tracker.Failed, strings.TrimSpace(string(res.Failure)+": "+res.Reason)
		} else if res.Note != "" {
			outcome += ": " + res.Note
		}
		stepDone := t.StepDone != "" && res.Status == t.StepDone
		if err := s.Tracker.FinishSession(rec.ID, status, outcome, res.CostUSD, stepDone); err != nil {
			return Result{}, err
		}
		res.Session = rec.ID
		if res.Failure == Infrastructure && attempt < s.Retries {
			continue
		}
		return res, nil
	}
}
