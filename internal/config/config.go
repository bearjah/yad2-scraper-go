package config

import (
	"encoding/json"
	"os"
)

type Project struct {
	Topic   string `json:"topic"`
	URL     string `json:"url"`
	Disabled bool  `json:"disabled,omitempty"`
}

type Telegram struct {
	Token  string `json:"token"`
	ChatID string `json:"chat_id"`
}

type Config struct {
	Projects []Project `json:"projects"`
	Telegram Telegram  `json:"telegram"`
}

func Load(path string) (Config, error) {
	var c Config
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	err = json.NewDecoder(f).Decode(&c)
	return c, err
}
