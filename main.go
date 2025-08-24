package main

import (
	"bytes"
	"context"
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
	"github.com/chromedp/chromedp"
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

func loadConfig(path string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(b, &cfg)
	return cfg, err
}

// plain HTTP fetch
func getYad2Response(url string) (string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome Safari")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "he-IL,he;q=0.9,en-US;q=0.8")
	req.Header.Set("Referer", "https://www.yad2.co.il/realestate/rent")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("status %d, %s", res.StatusCode, string(body))
	}
	body, _ := io.ReadAll(res.Body)
	return string(body), nil
}

// headless browser fetch
func getRenderedHTML(url string) (string, error) {
	// create allocator with custom UA
	// allocCtx, allocCancel := chromedp.NewExecAllocator(
	// 	context.Background(),
	// 	chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/124 Safari/537.36"),
	// 	chromedp.Flag("headless", true),
	// 	chromedp.Flag("disable-gpu", true),
	// )
	// in getRenderedHTML, allocator setup:
	allocCtx, allocCancel := chromedp.NewExecAllocator(
		context.Background(),
		chromedp.ExecPath(os.Getenv("CHROME_PATH")),
		chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/124 Safari/537.36"),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	defer allocCancel()

	// browser context
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// overall timeout
	ctx, cancel = context.WithTimeout(ctx, 40*time.Second)
	defer cancel()

	var html string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url),
		chromedp.Sleep(1200*time.Millisecond),
		chromedp.WaitReady(`[data-testid="feed-item"], .feeditem, article`, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// light scrolling to trigger lazy content
			for i := 0; i < 4; i++ {
				if err := chromedp.Run(ctx, chromedp.Evaluate(`window.scrollBy(0, 900)`, nil)); err != nil {
					return err
				}
				time.Sleep(350 * time.Millisecond)
			}
			return nil
		}),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	)
	return html, err
}

// scrape images, fallback to browser when blocked or empty
func scrapeItemsAndExtractImgUrls(url string) ([]string, error) {
	html, err := getYad2Response(url)
	if err != nil {
		return nil, err
	}
	imgs, blocked := parseImgURLs(html)

	if blocked || len(imgs) == 0 || strings.Contains(strings.ToLower(html), "shieldsquare") || strings.Contains(strings.ToLower(html), "captcha") || os.Getenv("YAD2_BROWSER") == "1" {
		rendered, rerr := getRenderedHTML(url)
		if rerr != nil {
			return nil, fmt.Errorf("bot detection, render failed, %v", rerr)
		}
		_ = os.WriteFile("debug_rendered.html", []byte(rendered), 0o644)
		imgs, blocked = parseImgURLs(rendered)
		if blocked {
			return nil, fmt.Errorf("bot detection after render")
		}
	}

	if len(imgs) == 0 {
		return nil, fmt.Errorf("could not find feed items")
	}
	return imgs, nil
}

// tolerant parser for images on real estate cards
func parseImgURLs(html string) ([]string, bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, false
	}
	title := strings.TrimSpace(doc.Find("title").First().Text())
	lt := strings.ToLower(title)
	if strings.Contains(lt, "shieldsquare") || strings.Contains(lt, "captcha") {
		return nil, true
	}

	var out []string
	doc.Find(`[data-testid="feed-item"] img, .feeditem img, article img`).Each(func(_ int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok && strings.TrimSpace(src) != "" {
			out = append(out, src)
			return
		}
		if src, ok := s.Attr("data-src"); ok && strings.TrimSpace(src) != "" {
			out = append(out, src)
			return
		}
		if srcset, ok := s.Attr("srcset"); ok && strings.TrimSpace(srcset) != "" {
			parts := strings.Fields(srcset)
			if len(parts) > 0 {
				out = append(out, parts[0])
			}
		}
	})
	return out, false
}

// state management, same as your Node version
func checkIfHasNewItem(imgUrls []string, topic string) ([]string, error) {
	dataDir := "data"
	filePath := filepath.Join(dataDir, topic+".json")

	var savedUrls []string
	if b, err := os.ReadFile(filePath); err == nil {
		_ = json.Unmarshal(b, &savedUrls)
	} else {
		_ = os.MkdirAll(dataDir, 0o755)
		_ = os.WriteFile(filePath, []byte("[]"), 0o644)
		savedUrls = []string{}
	}

	seen := make(map[string]bool, len(savedUrls))
	for _, u := range savedUrls {
		seen[u] = true
	}

	var newItems []string
	for _, u := range imgUrls {
		if !seen[u] {
			seen[u] = true
			savedUrls = append(savedUrls, u)
			newItems = append(newItems, u)
		}
	}

	if len(newItems) > 0 {
		b, _ := json.MarshalIndent(savedUrls, "", "  ")
		_ = os.WriteFile(filePath, b, 0o644)
		_ = os.WriteFile("push_me", []byte(""), 0o644)
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

func scrape(topic, url, apiToken, chatID string) error {
	if err := sendTelegram(apiToken, chatID, fmt.Sprintf("Starting scanning %s\n%s", topic, url)); err != nil {
		log.Printf("telegram warmup error, %v", err)
	}

	imgs, err := scrapeItemsAndExtractImgUrls(url)
	if err != nil {
		_ = sendTelegram(apiToken, chatID, fmt.Sprintf("Scan failed, %v", err))
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
