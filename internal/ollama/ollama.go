// Package ollama is a small client for a local Ollama server's POST /api/chat
// endpoint. The door uses it to stream a reply token by token.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
		return ChatResponse{}, chatTransportErr(ctx, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, statusError(resp)
	}
	if !req.Stream {
		var one ChatResponse
		if err := json.NewDecoder(resp.Body).Decode(&one); err != nil {
			return ChatResponse{}, fmt.Errorf("ollama chat: decode: %w: %w", ErrBadStream, err)
		}
		return one, nil
	}
	return readStream(ctx, resp.Body, onChunk)
}

// Ping asks GET /api/version. A network failure is ErrUnreachable.
// Any HTTP response means the server is up, even if the path is odd.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("ollama ping: nil client")
	}
	if ctx == nil {
		return fmt.Errorf("ollama ping: nil context")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/version", nil)
	if err != nil {
		return fmt.Errorf("ollama ping: %w: %w", ErrUnreachable, err)
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = http.DefaultClient
	}
	resp, err := httpc.Do(req)
	if err != nil {
		if ctx.Err() != nil && errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
			return fmt.Errorf("ollama ping: %w: %w", ErrUnreachable, err)
		}
		if ctx.Err() != nil {
			return fmt.Errorf("ollama ping: %w", ctx.Err())
		}
		return fmt.Errorf("ollama ping: %w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	return nil
}

// chatTransportErr classifies a failed Do or read.
// Context causes such as ErrStall win over the transport's wrapper.
// Anything else that stopped the dial is ErrUnreachable.
func chatTransportErr(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		cause := context.Cause(ctx)
		if cause != nil && !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
			return fmt.Errorf("ollama chat: %w", cause)
		}
		return fmt.Errorf("ollama chat: %w", ctx.Err())
	}
	return fmt.Errorf("ollama chat: %w: %w", ErrUnreachable, err)
}

// readStream reads NDJSON. One JSON object per line.
func readStream(ctx context.Context, body io.Reader, onChunk func(ChatResponse) error) (ChatResponse, error) {
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
			return ChatResponse{}, fmt.Errorf("ollama chat: decode: %w: %w", ErrBadStream, err)
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
		if ctx.Err() != nil {
			return ChatResponse{}, chatTransportErr(ctx, err)
		}
		return ChatResponse{}, fmt.Errorf("ollama chat: read: %w: %w", ErrBadStream, err)
	}
	if !saw {
		return ChatResponse{}, fmt.Errorf("ollama chat: %w", ErrBadStream)
	}
	last.Message.Content = full.String()
	if last.Message.Role == "" {
		last.Message.Role = RoleAssistant
	}
	return last, nil
}

func statusError(resp *http.Response) error {
	slurp, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	se := &StatusError{
		Code:   resp.StatusCode,
		Status: resp.Status,
		Body:   strings.TrimSpace(string(slurp)),
	}
	if err != nil {
		return fmt.Errorf("ollama chat: %w: %w", se, err)
	}
	return fmt.Errorf("ollama chat: %w", se)
}
