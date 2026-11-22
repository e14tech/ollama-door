// Package ollama is a small client for a local Ollama server's POST /api/chat
// endpoint. The door uses it to stream a reply token by token.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client talks to one Ollama server.
// Construct it with New. BaseURL has no trailing slash.
type Client struct {
	// BaseURL is the server origin, for example http://localhost:11434.
	BaseURL string

	// HTTP issues requests. New sets it to http.DefaultClient.
	// Leave Timeout at 0: a streaming reply can run longer than a
	// typical client timeout. Cancel the context passed to Chat instead.
	HTTP *http.Client
}

// New returns a Client for baseURL, such as http://localhost:11434.
func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    http.DefaultClient,
	}
}

// Chat posts req to {BaseURL}/api/chat.
//
// The returned Message.Content is the full assistant text: every streamed
// piece, joined in order. The other fields (Done, timings, model) come
// from the last object.
//
// When req.Stream is true, Ollama writes newline-delimited JSON. onChunk
// is called for each object as it arrives, including the final one, with
// that object's own Message.Content (one piece, not the joined text).
// A nil onChunk skips that call. When req.Stream is false, Ollama writes
// one JSON object and onChunk is not called.
//
// Cancel ctx to stop a reply that is still streaming.
func (c *Client) Chat(ctx context.Context, req ChatRequest, onChunk func(ChatResponse) error) (ChatResponse, error) {
	if c == nil {
		return ChatResponse{}, fmt.Errorf("ollama chat: nil client")
	}
	if ctx == nil {
		return ChatResponse{}, fmt.Errorf("ollama chat: nil context")
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ollama chat: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ollama chat: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpc := c.HTTP
	if httpc == nil {
		httpc = http.DefaultClient
	}
	resp, err := httpc.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("ollama chat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, statusError(resp)
	}
	if !req.Stream {
		var one ChatResponse
		if err := json.NewDecoder(resp.Body).Decode(&one); err != nil {
			return ChatResponse{}, fmt.Errorf("ollama chat: decode: %w", err)
		}
		return one, nil
	}
	return readStream(resp.Body, onChunk)
}

// readStream reads NDJSON. One JSON object per line.
func readStream(body io.Reader, onChunk func(ChatResponse) error) (ChatResponse, error) {
	sc := bufio.NewScanner(body)
	// Scanner stops at 64KiB by default. A single token line can be longer.
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	var full strings.Builder
	var last ChatResponse
	saw := false
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk ChatResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			return ChatResponse{}, fmt.Errorf("ollama chat: decode: %w", err)
		}
		saw = true
		full.WriteString(chunk.Message.Content)
		if onChunk != nil {
			if err := onChunk(chunk); err != nil {
				return ChatResponse{}, fmt.Errorf("ollama chat: %w", err)
			}
		}
		last = chunk
	}
	if err := sc.Err(); err != nil {
		return ChatResponse{}, fmt.Errorf("ollama chat: read: %w", err)
	}
	if !saw {
		return ChatResponse{}, fmt.Errorf("ollama chat: empty response")
	}
	last.Message.Content = full.String()
	if last.Message.Role == "" {
		last.Message.Role = RoleAssistant
	}
	return last, nil
}

func statusError(resp *http.Response) error {
	slurp, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.TrimSpace(string(slurp))
	if err != nil {
		return fmt.Errorf("ollama chat: status %s: %w", resp.Status, err)
	}
	if msg == "" {
		return fmt.Errorf("ollama chat: status %s", resp.Status)
	}
	return fmt.Errorf("ollama chat: status %s: %s", resp.Status, msg)
}
