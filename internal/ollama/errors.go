package ollama

import (
	"errors"
	"fmt"
	"net/http"
)

// Sentinel errors for the failures the door explains to a caller.
// Chat wraps them with fmt.Errorf and %w, so errors.Is still finds them.
var (
	// ErrUnreachable means the server could not be contacted
	// (connection refused, DNS, or no route).
	ErrUnreachable = errors.New("ollama server is unreachable")

	// ErrModelNotFound means the server answered 404 for the model.
	ErrModelNotFound = errors.New("ollama model not found")

	// ErrBadStream means the body was not the NDJSON Chat expects.
	ErrBadStream = errors.New("ollama sent a bad stream")

	// ErrStall means the server made no progress before the deadline.
	// The door cancels the request context with this cause.
	ErrStall = errors.New("ollama made no progress")
)

// StatusError is a non-200 response from Ollama.
// A 404 unwraps to ErrModelNotFound so errors.Is can see both facts.
type StatusError struct {
	Code   int
	Status string
	Body   string
}

func (e *StatusError) Error() string {
	if e == nil {
		return "ollama status"
	}
	if e.Body == "" {
		return "ollama status " + e.Status
	}
	return "ollama status " + e.Status + ": " + e.Body
}

// Unwrap lets errors.Is(err, ErrModelNotFound) succeed for a 404.
func (e *StatusError) Unwrap() error {
	if e != nil && e.Code == http.StatusNotFound {
		return ErrModelNotFound
	}
	return nil
}

func statusErr(code int, status, body string) error {
	return fmt.Errorf("ollama chat: %w", &StatusError{
		Code:   code,
		Status: status,
		Body:   body,
	})
}
