// Package clients holds external-API wrappers used by ranking-svc.
//
// The Claude client is intentionally tiny — we POST a single Messages API
// call and parse the JSON Claude returns. No SDK; the wire format is
// stable enough that a 60-line client beats a transitive dep.
package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	anthropicEndpoint = "https://api.anthropic.com/v1/messages"
	anthropicVersion  = "2023-06-01"
)

// Claude wraps Anthropic's Messages API. Build it once at boot if the
// ANTHROPIC_API_KEY env is set; nil means "fall back to rule-based".
type Claude struct {
	apiKey string
	model  string
	http   *http.Client
}

// NewClaude returns nil if apiKey is empty (so callers can do
// `if c == nil { fallback }` without nil checks every place). model is
// optional — empty defaults to the cheapest fast model.
func NewClaude(apiKey, model string) *Claude {
	if apiKey == "" {
		return nil
	}
	if model == "" {
		// Haiku 4.5: fastest + cheapest in the Claude 4 family. Plenty for
		// "rank 5-10 apartments and write 2-3 reasons each".
		model = "claude-haiku-4-5-20251001"
	}
	return &Claude{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// MessagesRequest is the wire shape for a single-turn user message.
type messagesRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	System    string         `json:"system,omitempty"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete sends a system + user prompt and returns the concatenated text
// response. Errors include API-level failures (4xx/5xx with Anthropic
// error envelope) and HTTP/IO failures.
func (c *Claude) Complete(ctx context.Context, system, user string, maxTokens int) (string, error) {
	if maxTokens == 0 {
		maxTokens = 2048
	}
	body, err := json.Marshal(messagesRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  []claudeMessage{{Role: "user", Content: user}},
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call anthropic: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var parsed messagesResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		// Some 5xx responses come back as plain text; surface the raw body.
		return "", fmt.Errorf("decode response (status %d): %w: %s", resp.StatusCode, err, string(respBytes))
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("anthropic error: %s — %s", parsed.Error.Type, parsed.Error.Message)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("anthropic http %d: %s", resp.StatusCode, string(respBytes))
	}

	var sb strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), nil
}
