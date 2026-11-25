package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewTrimsSlash(t *testing.T) {
	c := New("http://localhost:11434/")
	if c.BaseURL != "http://localhost:11434" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
}

func TestChatStream(t *testing.T) {
	var got ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("path %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type %s", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		io.WriteString(w, "{\"model\":\"llama3.2\",\"message\":{\"role\":\"assistant\",\"content\":\"Hello\"},\"done\":false}\n")
		io.WriteString(w, "\n")
		io.WriteString(w, "{\"model\":\"llama3.2\",\"message\":{\"role\":\"assistant\",\"content\":\" world\"},\"done\":false}\n")
		io.WriteString(w, "{\"model\":\"llama3.2\",\"message\":{\"role\":\"assistant\",\"content\":\"\"},\"done\":true,\"done_reason\":\"stop\"}\n")
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.HTTP = srv.Client()

	var chunks []string
	resp, err := c.Chat(context.Background(), ChatRequest{
		Model:    "llama3.2",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Stream:   true,
	}, func(ch ChatResponse) error {
		chunks = append(chunks, ch.Message.Content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stream || got.Model != "llama3.2" {
		t.Fatalf("request = %+v", got)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "hi" {
		t.Fatalf("messages = %+v", got.Messages)
	}
	wantChunks := []string{"Hello", " world", ""}
	if strings.Join(chunks, "|") != strings.Join(wantChunks, "|") {
		t.Fatalf("chunks = %#v", chunks)
	}
	if resp.Message.Content != "Hello world" {
		t.Fatalf("full message = %q", resp.Message.Content)
	}
	if !resp.Done || resp.DoneReason != "stop" {
		t.Fatalf("final = %+v", resp)
	}
}

func TestChatLongLine(t *testing.T) {
	big := strings.Repeat("x", 80*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := json.Marshal(ChatResponse{
			Model:   "m",
			Message: Message{Role: RoleAssistant, Content: big},
			Done:    true,
		})
		if err != nil {
			t.Errorf("marshal: %v", err)
		}
		w.Write(append(payload, '\n'))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.HTTP = srv.Client()
	resp, err := c.Chat(context.Background(), ChatRequest{Model: "m", Stream: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.Content != big {
		t.Fatalf("len %d, want %d", len(resp.Message.Content), len(big))
	}
}

func TestChatStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.HTTP = srv.Client()
	_, err := c.Chat(context.Background(), ChatRequest{Model: "missing", Stream: true}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestChatContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		io.WriteString(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"Hi\"},\"done\":false}\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.HTTP = srv.Client()
	ctx, cancel := context.WithCancel(context.Background())
	_, err := c.Chat(ctx, ChatRequest{Model: "m", Stream: true}, func(ChatResponse) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestChatChunkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{\"message\":{\"role\":\"assistant\",\"content\":\"x\"},\"done\":false}\n")
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.HTTP = srv.Client()
	want := errors.New("stop")
	_, err := c.Chat(context.Background(), ChatRequest{Model: "m", Stream: true}, func(ChatResponse) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func TestChatNotStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{
			Message: Message{Role: RoleAssistant, Content: "full"},
			Done:    true,
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.HTTP = srv.Client()
	resp, err := c.Chat(context.Background(), ChatRequest{Model: "m", Stream: false}, func(ChatResponse) error {
		t.Error("onChunk should not run for a single object")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.Content != "full" || !resp.Done {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestChatBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "not-json\n")
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.HTTP = srv.Client()
	_, err := c.Chat(context.Background(), ChatRequest{Model: "m", Stream: true}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
}
