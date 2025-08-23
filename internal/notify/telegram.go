package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Telegram struct {
	Token  string
	ChatID string
}

func (t Telegram) Send(text string) error {
	if t.Token == "" || t.ChatID == "" {
		return nil
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.Token)
	body := map[string]any{
		"chat_id":    t.ChatID,
		"text":       text,
		"parse_mode": "HTML",
		"disable_web_page_preview": false,
	}
	b, _ := json.Marshal(body)
	res, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("telegram status %d", res.StatusCode)
	}
	return nil
}
