package summarize

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/gin-gonic/gin"
)

const systemPrompt = `You are a log analysis expert.
Given a batch of application log entries, produce a concise summary that covers:
1. Key events and what the system was doing
2. Any errors or warnings, and their likely cause
3. Patterns or anomalies worth noting
4. Recommended actions (if any)

Be direct and technical. Use bullet points. Do not repeat raw log lines verbatim.`

// SummarizeRequest is the JSON body accepted by POST /summarize.
type SummarizeRequest struct {
	MachineID string   `json:"machine_id"`
	FilePath  string   `json:"file_path"`
	LogLines  []string `json:"log_lines"`
}

// SummarizeResponse is returned to the caller.
type SummarizeResponse struct {
	MachineID string `json:"machine_id"`
	FilePath  string `json:"file_path"`
	LogCount  int    `json:"log_count"`
	Summary   string `json:"summary"`
}

// Handler handles summarize requests.
type Handler struct {
	client anthropic.Client
}

func NewHandler(apiKey string) *Handler {
	if apiKey != "" {
		return &Handler{client: anthropic.NewClient(option.WithAPIKey(apiKey))}
	}
	return &Handler{client: anthropic.NewClient()}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.POST("/summarize", h.summarize)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}

// POST /summarize
func (h *Handler) summarize(c *gin.Context) {
	var req SummarizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if len(req.LogLines) == 0 {
		c.JSON(http.StatusOK, SummarizeResponse{
			MachineID: req.MachineID,
			FilePath:  req.FilePath,
			LogCount:  0,
			Summary:   "No log lines provided.",
		})
		return
	}

	summary, err := h.callClaude(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, SummarizeResponse{
		MachineID: req.MachineID,
		FilePath:  req.FilePath,
		LogCount:  len(req.LogLines),
		Summary:   summary,
	})
}

func (h *Handler) callClaude(ctx context.Context, req SummarizeRequest) (string, error) {
	const maxLines = 500
	lines := req.LogLines
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Machine: %s\nFile: %s\n\nAnalyze these %d log entries:\n\n",
		req.MachineID, req.FilePath, len(lines)))
	for _, l := range lines {
		sb.WriteString(l + "\n")
	}

	adaptive := anthropic.ThinkingConfigAdaptiveParam{}

	stream := h.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeOpus4_6,
		MaxTokens: 2048,
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		System: []anthropic.TextBlockParam{{
			Text:         systemPrompt,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(sb.String())),
		},
	})

	msg := anthropic.Message{}
	for stream.Next() {
		msg.Accumulate(stream.Current())
	}
	if err := stream.Err(); err != nil {
		return "", fmt.Errorf("claude error: %w", err)
	}

	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			return tb.Text, nil
		}
	}
	return "", fmt.Errorf("no text in claude response")
}
