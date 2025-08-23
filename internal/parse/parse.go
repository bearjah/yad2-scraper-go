package parse

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Item struct {
	ID       string
	Title    string
	Price    string
	Location string
	URL      string
	ImageURL string
}

func FromHTML(docHTML []byte) ([]Item, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(docHTML)))
	if err != nil {
		return nil, err
	}

	var out []Item
	// selector can change, you will likely need to adjust after inspecting page HTML
	doc.Find("[data-testid='feed-item'] , .feeditem").Each(func(_ int, s *goquery.Selection) {
		id, _ := s.Attr("id")
		title := strings.TrimSpace(s.Find("[data-testid='item-title'], .title").Text())
		price := strings.TrimSpace(s.Find("[data-testid='item-price'], .price").Text())
		location := strings.TrimSpace(s.Find("[data-testid='item-location'], .location").Text())

		linkSel := s.Find("a[href]").First()
		href, _ := linkSel.Attr("href")
		imgSel := s.Find("img[src]").First()
		img, _ := imgSel.Attr("src")

		if id == "" && href != "" {
			id = href
		}
		if id != "" {
			out = append(out, Item{
				ID:       id,
				Title:    title,
				Price:    price,
				Location: location,
				URL:      href,
				ImageURL: img,
			})
		}
	})
	return out, nil
}

