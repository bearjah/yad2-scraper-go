package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Seen struct {
	IDs map[string]bool `json:"ids"`
}

func filePath(baseDir, topic string) string {
	safe := topic
	return filepath.Join(baseDir, safe+".json")
}

func Load(baseDir, topic string) (Seen, error) {
	path := filePath(baseDir, topic)
	var s Seen
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Seen{IDs: map[string]bool{}}, nil
		}
		return s, err
	}
	err = json.Unmarshal(b, &s)
	if s.IDs == nil {
		s.IDs = map[string]bool{}
	}
	return s, err
}

func Save(baseDir, topic string, s Seen) error {
	if s.IDs == nil {
		s.IDs = map[string]bool{}
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath(baseDir, topic), b, 0o644)
}
