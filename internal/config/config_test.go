package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ollama-door.json")
	body := []byte(`{"model":"llama3.2","width":40,"logchat":true}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model == nil || *cfg.Model != "llama3.2" {
		t.Fatalf("model %+v", cfg.Model)
	}
	if cfg.Width == nil || *cfg.Width != 40 {
		t.Fatalf("width %+v", cfg.Width)
	}
	if cfg.Host != nil {
		t.Fatal("missing host should stay nil")
	}
	if cfg.LogChat == nil || !*cfg.LogChat {
		t.Fatalf("logchat %+v", cfg.LogChat)
	}
}

func TestLoadUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"colour":"green"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown key to fail")
	}
}
