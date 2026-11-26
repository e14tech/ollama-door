// Package config reads the optional JSON file a sysop uses to set
// door defaults. Command-line flags still win when they are typed.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// File is the JSON object in -config.
// A nil pointer means the key was left out, so the door keeps the
// built-in default (or a flag) instead of treating 0 or "" as a choice.
type File struct {
	Host    *string `json:"host"`
	Model   *string `json:"model"`
	System  *string `json:"system"`
	Theme   *string `json:"theme"`
	Width   *int    `json:"width"`
	Idle    *string `json:"idle"`
	Timeout *string `json:"timeout"`
	MaxMin  *int    `json:"maxmin"`
	Log     *string `json:"log"`
	LogChat *bool   `json:"logchat"`
}

// Load reads a JSON config file. Unknown keys are an error so a typo
// does not silently do nothing.
func Load(path string) (File, error) {
	f, err := os.Open(path)
	if err != nil {
		return File{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var cfg File
	if err := dec.Decode(&cfg); err != nil {
		return File{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}
