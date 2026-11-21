// Package ollama is a small client for a local Ollama server's POST /api/chat
// endpoint. The door uses it to stream a reply token by token.
package ollama

import (
	"context"
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

// Chat posts req to {BaseURL}/api/chat and returns the final response,
// the object whose Done field is true.
//
// When req.Stream is true, Ollama writes newline-delimited JSON. onChunk
// is called for each object, including the final one. A nil onChunk skips
// that call. When req.Stream is false, Ollama writes one JSON object and
// onChunk is not called.
//
// Cancel ctx to stop a reply that is still streaming.
//
// TODO: encode req, POST it, and decode the body as described above.
// The stub returns a zero ChatResponse and a nil error so the package
// compiles. Do not call it from main until the body is real.
func (c *Client) Chat(ctx context.Context, req ChatRequest, onChunk func(ChatResponse) error) (ChatResponse, error) {
	// Keep the parameters referenced so this stub compiles.
	// Delete these lines when you write the body.
	_ = c
	_ = ctx
	_ = req
	_ = onChunk
	return ChatResponse{}, nil
}
