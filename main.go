package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Config mirrors config.json
type Config struct {
	Telegram struct {
		Token  string `json:"token"`
		ChatID string `json:"chat_id"`
	} `json:"telegram"`
	Projects []Project `json:"projects"`
}

type Project struct {
	Topic    string `json:"topic"`
	URL      string `json:"url"`
	Disabled bool   `json:"disabled"`
}

// Load config.json
func loadConfig(path string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(b, &cfg)
	return cfg, err
}

// Fetch HTML with headers
func getYad2Response(url string) (string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept-Language", "he-IL,he;q=0.9,en-US;q=0.8")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("status %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	return string(body), nil
}

// Parse HTML and extract image URLs
func scrapeItemsAndExtractImgUrls(url string) ([]string, error) {
	html, err := getYad2Response(url)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	// detect bot block
	if strings.Contains(doc.Find("title").First().Text(), "ShieldSquare Captcha") {
		return nil, fmt.Errorf("bot detection")
	}

	var imageUrls []string
	doc.Find(".feeditem .pic").Each(func(_ int, s *goquery.Selection) {
		if src, ok := s.Find("img").Attr("src"); ok && src != "" {
			imageUrls = append(imageUrls, src)
		}
	})
	if len(imageUrls) == 0 {
		return nil, fmt.Errorf("could not find feed items")
	}
	return imageUrls, nil
}

// Load or create ./data/topic.json and check for new items
func checkIfHasNewItem(imgUrls []string, topic string) ([]string, error) {
	dataDir := "data"
	filePath := filepath.Join(dataDir, topic+".json")

	var savedUrls []string
	if b, err := os.ReadFile(filePath); err == nil {
		_ = json.Unmarshal(b, &savedUrls)
	} else {
		_ = os.MkdirAll(dataDir, 0755)
		_ = os.WriteFile(filePath, []byte("[]"), 0644)
		savedUrls = []string{}
	}

	shouldUpdate := false
	newItems := []string{}

	// add new
	for _, u := range imgUrls {
		found := false
		for _, s := range savedUrls {
			if s == u {
				found = true
				break
			}
		}
		if !found {
			savedUrls = append(savedUrls, u)
			newItems = append(newItems, u)
			shouldUpdate = true
		}
	}

	if shouldUpdate {
		b, _ := json.MarshalIndent(savedUrls, "", "  ")
		_ = os.WriteFile(filePath, b, 0644)
		// emulate push flag
		_ = os.WriteFile("push_me", []byte(""), 0644)
	}

	return newItems, nil
}

func sendTelegram(apiToken, chatID, text string) error {
	apiToken = strings.TrimSpace(apiToken)
	chatID = strings.TrimSpace(chatID)
	if apiToken == "" || chatID == "" {
		return fmt.Errorf("missing token or chat id")
	}

	url := "https://api.telegram.org/bot" + apiToken + "/sendMessage"
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": false,
	}
	b, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("telegram status %d, %s", res.StatusCode, string(body))
	}
	return nil
}

// Scrape one project
func scrape(topic, url, apiToken, chatID string) error {
	if err := sendTelegram(apiToken, chatID, fmt.Sprintf("Starting scanning %s\n%s", topic, url)); err != nil {
		log.Printf("telegram warmup error: %v", err)
	}

	imgs, err := scrapeItemsAndExtractImgUrls(url)
	if err != nil {
		sendTelegram(apiToken, chatID, fmt.Sprintf("Scan failed: %v", err))
		return err
	}
	newItems, _ := checkIfHasNewItem(imgs, topic)
	if len(newItems) > 0 {
		msg := fmt.Sprintf("%d new items:\n%s", len(newItems), strings.Join(newItems, "\n----------\n"))
		return sendTelegram(apiToken, chatID, msg)
	}
	return sendTelegram(apiToken, chatID, "No new items were added")
}

func main() {
	cfg, err := loadConfig("config.json")
	if err != nil {
		log.Fatal(err)
	}
	apiToken := os.Getenv("API_TOKEN")
	chatID := os.Getenv("CHAT_ID")
	if apiToken == "" {
		apiToken = cfg.Telegram.Token
	}
	if chatID == "" {
		chatID = cfg.Telegram.ChatID
	}

	for _, p := range cfg.Projects {
		if p.Disabled {
			log.Printf("Topic %s is disabled, skipping", p.Topic)
			continue
		}
		if err := scrape(p.Topic, p.URL, apiToken, chatID); err != nil {
			log.Printf("scrape error %v", err)
		}
	}
}
