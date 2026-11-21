package ollama

import "time"

// Role values for Message.Role. Ollama expects these exact strings.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is one turn in a chat, matching Ollama's message object.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the JSON body of POST /api/chat.
//
// A streaming request looks like:
//
//	{"model":"llama3.2","messages":[{"role":"user","content":"hi"}],"stream":true}
//
// Stream true asks for NDJSON (one JSON object per line). Stream false
// asks for a single JSON object. The field is always sent; Ollama's
// default when the field is omitted is to stream.
type ChatRequest struct {
	Model    string         `json:"model"`
	Messages []Message      `json:"messages"`
	Stream   bool           `json:"stream"`
	Options  map[string]any `json:"options,omitempty"`
	// KeepAlive is a duration Ollama understands, such as "5m".
	// Empty omits the field and leaves the server default.
	KeepAlive string `json:"keep_alive,omitempty"`
}

// ChatResponse is one JSON object from POST /api/chat.
//
// While a streamed reply is in progress, Done is false and Message.Content
// holds the next piece of text (often a single token). The last object has
// Done set, usually an empty Message.Content, and the timing fields filled in.
// A non-streaming call returns one object with the full reply and Done set.
//
// Duration fields are nanoseconds, which is the unit Ollama puts in JSON.
// created_at is RFC3339, decoded into CreatedAt by encoding/json.
type ChatResponse struct {
	Model      string    `json:"model"`
	CreatedAt  time.Time `json:"created_at"`
	Message    Message   `json:"message"`
	Done       bool      `json:"done"`
	DoneReason string    `json:"done_reason,omitempty"`

	TotalDuration      int64 `json:"total_duration,omitempty"`
	LoadDuration       int64 `json:"load_duration,omitempty"`
	PromptEvalCount    int   `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64 `json:"prompt_eval_duration,omitempty"`
	EvalCount          int   `json:"eval_count,omitempty"`
	EvalDuration       int64 `json:"eval_duration,omitempty"`
}
