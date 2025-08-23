package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/bearjah/yad2-scraper-go/internal/config"
	"github.com/bearjah/yad2-scraper-go/internal/fetch"
	"github.com/bearjah/yad2-scraper-go/internal/notify"
	"github.com/bearjah/yad2-scraper-go/internal/parse"
	"github.com/bearjah/yad2-scraper-go/internal/store"
)

func main() {
	cfgPath := env("YAD2GO_CONFIG", "config.json")
	dataDir := env("YAD2GO_DATA", "data")
	pages := envInt("YAD2GO_PAGES", 1)

	cfg, err := config.Load(cfgPath)
	must(err)

	tg := notify.Telegram{
		Token:  firstNonEmpty(os.Getenv("TELEGRAM_TOKEN"), cfg.Telegram.Token),
		ChatID: firstNonEmpty(os.Getenv("TELEGRAM_CHAT_ID"), cfg.Telegram.ChatID),
	}

	if err := tg.Send("bot online, starting run"); err != nil {
		log.Printf("telegram warmup error, %v", err)
	}

	for _, p := range cfg.Projects {
		if p.Disabled {
			continue
		}
		log.Printf("topic %s", p.Topic)

		var items []parse.Item
		for page := 1; page <= pages; page++ {
			html, err := fetchHTMLPage(p.URL, page)
			if err != nil {
				log.Printf("fetch error, page %d, %v", page, err)
				continue
			}
			// snapshot for inspection
			_ = os.WriteFile(snapshotName(p.Topic, page), html, 0o644)

			its, err := parse.FromHTML(html)
			if err != nil {
				log.Printf("parse error, page %d, %v", page, err)
				continue
			}
			items = append(items, its...)
		}

		log.Printf("parsed %d items", len(items))
		for i := 0; i < len(items) && i < 3; i++ {
			it := items[i]
			log.Printf("sample %d, id=%q, title=%q, price=%q, location=%q, url=%q",
				i+1, it.ID, it.Title, it.Price, it.Location, absolute(it.URL))
		}

		// normalize ids, avoid empty ids by hashing the absolute url
		for i := range items {
			if strings.TrimSpace(items[i].ID) == "" {
				items[i].ID = hashID(absolute(items[i].URL))
			}
		}

		seen, err := store.Load(dataDir, p.Topic)
		must(err)

		var newOnes []parse.Item
		for _, it := range items {
			if it.ID == "" {
				continue
			}
			if !seen.IDs[it.ID] {
				newOnes = append(newOnes, it)
				seen.IDs[it.ID] = true
			}
		}
		log.Printf("new items %d", len(newOnes))
		if len(newOnes) == 0 {
			continue
		}

		must(store.Save(dataDir, p.Topic, seen))
		for _, msg := range chunkMessages(p.Topic, newOnes, 10) {
			if err := tg.Send(msg); err != nil {
				log.Printf("telegram error, %v", err)
			}
		}
	}
}

func chunkMessages(topic string, items []parse.Item, capN int) []string {
	var out []string
	for i := 0; i < len(items); i += capN {
		j := i + capN
		if j > len(items) {
			j = len(items)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "New Yad2 items for %s\n", topic)
		for _, it := range items[i:j] {
			fmt.Fprintf(&b, "\n%s\n%s\n%s\n%s\n",
				strings.TrimSpace(it.Title),
				strings.TrimSpace(it.Price),
				strings.TrimSpace(it.Location),
				absolute(it.URL))
		}
		if j < len(items) {
			fmt.Fprintf(&b, "\n...and %d more", len(items)-j)
		}
		out = append(out, b.String())
	}
	return out
}

func fetchHTMLPage(base string, page int) ([]byte, error) {
	// simple pagination param, safe if the site uses page query
	if page <= 1 {
		return fetch.GetHTML(base)
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return fetch.GetHTML(base + sep + "page=" + strconv.Itoa(page))
}

func hashID(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:8])
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return d
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func absolute(u string) string {
	if strings.HasPrefix(u, "http") {
		return u
	}
	if u == "" {
		return ""
	}
	return "https://www.yad2.co.il" + u
}
func snapshotName(topic string, page int) string {
	safe := strings.NewReplacer(" ", "_", "/", "_").Replace(topic)
	return fmt.Sprintf("debug_%s_p%d.html", safe, page)
}
