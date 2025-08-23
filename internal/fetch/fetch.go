package fetch

import (
	"errors"
	"io"
	"net/http"
	"time"
)

var client = &http.Client{
	Timeout: 20 * time.Second,
}

func GetHTML(u string) ([]byte, error) {
	// one retry with backoff
	var last error
	for i := 0; i < 2; i++ {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			return nil, err
		}
		// modest headers to look like a real browser
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome Safari")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "he-IL,he,en-US,en;q=0.5")

		res, err := client.Do(req)
		if err != nil {
			last = err
		} else {
			defer res.Body.Close()
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				return io.ReadAll(res.Body)
			}
			last = errors.New(res.Status)
		}
		time.Sleep(time.Duration(500*(i+1)) * time.Millisecond)
	}
	return nil, last
}
