// Package agent implements an AI agent that monitors logs in real time.
//
// The heart of it is a classic agentic harness: a loop that hands Claude a set
// of tools, lets it decide which to call, executes the calls, feeds the results
// back, and repeats until the model stops asking for tools and produces a final
// report. This is the same shape as Claude Code's own tool-use loop, scoped down
// to log analysis.
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const defaultModel = anthropic.ModelClaudeOpus4_6

const systemPrompt = `You are an SRE log-monitoring agent watching a distributed system in real time.

You are given a window of recently-ingested log lines and a set of tools for
investigating the log store. Your job:
1. Triage the window — what is the system doing? Any errors, warnings, anomalies?
2. Investigate with your tools. Don't guess: if you suspect an incident, use
   count_logs / query_logs to measure its scope and pull concrete evidence.
   Start broad (level_breakdown) then drill in.
3. Decide a severity and report.

Be efficient — make only the tool calls you need, then stop and report. When you
are done investigating, respond with a final report in EXACTLY this format:

SEVERITY: <ok|info|warning|critical>
HEADLINE: <one line>
FINDINGS:
- <bullet, cite log ids / counts as evidence>
RECOMMENDED ACTIONS:
- <bullet, or "none">

Use "ok" when nothing needs attention. Reserve "critical" for active incidents
(error spikes, cascading failures, security events).`

// Severity levels parsed out of the model's final report.
const (
	SeverityOK       = "ok"
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// ToolCall records a single tool invocation in the harness — useful for showing
// the user *how* the agent reached its conclusion.
type ToolCall struct {
	Name   string `json:"name"`
	Input  string `json:"input"`
	Result string `json:"result"`
	IsErr  bool   `json:"is_error"`
}

// Report is the structured outcome of one agent run.
type Report struct {
	Severity  string     `json:"severity"`
	Headline  string     `json:"headline"`
	Report    string     `json:"report"`     // full final text from the model
	ToolCalls []ToolCall `json:"tool_calls"` // the harness transcript
	Steps     int        `json:"steps"`      // model turns taken
	StartedAt time.Time  `json:"started_at"`
	Duration  string     `json:"duration"`
}

// Agent wraps the Anthropic client, the tool executor, and harness config.
type Agent struct {
	client   anthropic.Client
	exec     *executor
	model    anthropic.Model
	maxSteps int
}

// New builds an agent over the given log store. apiKey may be empty, in which
// case the SDK falls back to ANTHROPIC_API_KEY.
func New(apiKey string, store LogStore) *Agent {
	var client anthropic.Client
	if apiKey != "" {
		client = anthropic.NewClient(option.WithAPIKey(apiKey))
	} else {
		client = anthropic.NewClient()
	}
	return &Agent{
		client:   client,
		exec:     &executor{store: store},
		model:    defaultModel,
		maxSteps: 8, // safety cap so a confused model can't loop forever
	}
}

// WithModel overrides the default model (e.g. a cheaper one for frequent ticks).
func (a *Agent) WithModel(m anthropic.Model) *Agent {
	if m != "" {
		a.model = m
	}
	return a
}

// Run executes the harness loop for one task and returns a structured report.
// The loop: ask the model → if it wants tools, run them and feed results back →
// repeat until it stops asking (or we hit maxSteps).
func (a *Agent) Run(ctx context.Context, task string) (Report, error) {
	started := time.Now()
	report := Report{Severity: SeverityOK, StartedAt: started.UTC()}

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(task)),
	}

	var finalText string

	for step := 0; step < a.maxSteps; step++ {
		report.Steps = step + 1

		msg, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     a.model,
			MaxTokens: 2048,
			// The system prompt and tool catalogue never change between calls,
			// so cache the prefix — every tick after the first is a cache hit.
			System: []anthropic.TextBlockParam{{
				Text:         systemPrompt,
				CacheControl: anthropic.NewCacheControlEphemeralParam(),
			}},
			Tools:    toolDefs,
			Messages: messages,
		})
		if err != nil {
			return report, fmt.Errorf("claude call (step %d): %w", step+1, err)
		}

		// Record the assistant turn so the model sees its own prior tool calls.
		messages = append(messages, msg.ToParam())

		// Capture any text the model emitted this turn.
		for _, block := range msg.Content {
			if tb, ok := block.AsAny().(anthropic.TextBlock); ok && tb.Text != "" {
				finalText = tb.Text
			}
		}

		// If the model isn't asking for tools, it's done.
		if msg.StopReason != anthropic.StopReasonToolUse {
			break
		}

		// Execute every tool the model requested this turn and gather results.
		var results []anthropic.ContentBlockParamUnion
		for _, block := range msg.Content {
			tu, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}
			out, isErr := a.exec.execute(ctx, tu.Name, tu.Input)
			report.ToolCalls = append(report.ToolCalls, ToolCall{
				Name:   tu.Name,
				Input:  string(tu.Input),
				Result: out,
				IsErr:  isErr,
			})
			results = append(results, anthropic.NewToolResultBlock(tu.ID, out, isErr))
		}

		// Feed the tool results back as the next user turn and loop.
		messages = append(messages, anthropic.NewUserMessage(results...))
	}

	report.Report = finalText
	report.Headline = extractField(finalText, "HEADLINE:")
	report.Severity = normalizeSeverity(extractField(finalText, "SEVERITY:"))
	report.Duration = time.Since(started).Round(time.Millisecond).String()
	return report, nil
}

// extractField pulls the value after a "LABEL:" line from the report text.
func extractField(text, label string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), strings.ToUpper(label)) {
			return strings.TrimSpace(trimmed[len(label):])
		}
	}
	return ""
}

func normalizeSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case SeverityCritical:
		return SeverityCritical
	case SeverityWarning:
		return SeverityWarning
	case SeverityInfo:
		return SeverityInfo
	default:
		return SeverityOK
	}
}
