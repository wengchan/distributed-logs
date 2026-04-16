package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const systemPrompt = `You are a log analysis expert.
Given a batch of application log entries, produce a concise summary that covers:
1. Key events and what the system was doing
2. Any errors or warnings, and their likely cause
3. Patterns or anomalies worth noting
4. Recommended actions (if any)

Be direct and technical. Use bullet points. Do not repeat raw log lines verbatim.`

// Summarizer sends log lines to Claude and returns a summary.
type Summarizer struct {
	client anthropic.Client
}

func NewSummarizer(apiKey string) *Summarizer {
	if apiKey != "" {
		return &Summarizer{client: anthropic.NewClient(option.WithAPIKey(apiKey))}
	}
	// Falls back to ANTHROPIC_API_KEY env var automatically.
	return &Summarizer{client: anthropic.NewClient()}
}

// Summarize sends logs to Claude Opus and returns a plain-text summary.
// The system prompt is marked for prompt caching — it stays identical across
// all calls so the cache prefix is always warm after the first request.
func (s *Summarizer) Summarize(ctx context.Context, logs []LogRow) (string, error) {
	if len(logs) == 0 {
		return "No log entries matched the query.", nil
	}

	userMsg := buildUserMessage(logs)

	// Adaptive thinking: Claude decides when and how much to think.
	adaptive := anthropic.ThinkingConfigAdaptiveParam{}

	// Stream the response — summaries can be long and we want to avoid timeouts.
	stream := s.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeOpus4_6,
		MaxTokens: 4096,
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		// Cache the system prompt — it never changes between requests.
		System: []anthropic.TextBlockParam{{
			Text:         systemPrompt,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userMsg)),
		},
	})

	// Accumulate the full streamed message.
	msg := anthropic.Message{}
	for stream.Next() {
		msg.Accumulate(stream.Current())
	}
	if err := stream.Err(); err != nil {
		return "", fmt.Errorf("claude stream error: %w", err)
	}

	// Extract the first text block from the response.
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			return tb.Text, nil
		}
	}
	return "", fmt.Errorf("no text block in claude response")
}

// buildUserMessage formats log rows into a prompt-friendly string.
// Capped at 500 lines to stay within reasonable token limits.
func buildUserMessage(logs []LogRow) string {
	const maxLines = 500
	if len(logs) > maxLines {
		logs = logs[:maxLines]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Analyze these %d log entries:\n\n", len(logs)))
	for _, l := range logs {
		sb.WriteString(fmt.Sprintf("[%s] %s  %s  %s\n",
			l.StartTime.Format("2006-01-02 15:04:05"),
			l.Level,
			l.MachineID,
			l.Message,
		))
	}
	return sb.String()
}
