package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

//go:embed web
var content embed.FS

var ErrRateLimitExceeded = errors.New("rate limited is exceeeded")

func main() {
	sub, _ := fs.Sub(content, "web") // StripPrefix, but for FS

	apiKey := os.Getenv("RAPIDAPI_KEY")
	if apiKey == "" {
		log.Fatal("RAPIDAPI_KEY must be provided.")
	}

	http.Handle("/", http.FileServer(http.FS(sub)))
	http.HandleFunc("/search", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(rw, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		outFile, err := os.Create(fmt.Sprintf("%v.txt", time.Now().UnixNano()))
		if err != nil {
			panic(err)
		}
		defer outFile.Close()

		var (
			keywords = strings.Split(r.PostFormValue("keywords"), "\r\n")
			lr       = r.PostFormValue("lr")
			cr       = r.PostFormValue("cr")
			num      = r.PostFormValue("num")
		)

		for _, kw := range keywords {
			if err := func() error {
				kw = strings.TrimSpace(kw)
				if kw == "" {
					return nil // continue
				}

				q := make(url.Values)
				q.Set("q", kw)
				q.Set("lr", lr)
				q.Set("cr", cr)
				q.Set("num", num)

				ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
				defer cancel()

				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://rapidapi.p.rapidapi.com/api/v1/search/"+q.Encode(), nil)
				req.Header.Set("X-RapidAPI-Host", "google-search3.p.rapidapi.com")
				req.Header.Set("X-RapidAPI-Key", apiKey)

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return fmt.Errorf("http request failed: kw=%q: %w", kw, err)
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusTooManyRequests {
					return ErrRateLimitExceeded
				}

				if resp.StatusCode != http.StatusOK {
					resp.Write(os.Stderr)
					return fmt.Errorf("http status code is not okay (%q): kw=%q: response dumped to stderr", resp.StatusCode, kw)
				}

				var v struct {
					Results []struct {
						Link string
					}
				}
				if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
					return fmt.Errorf("failed to decode json response from api: kw=%q: %w", kw, err)
				}

				// Append all links to the out file.
				for _, result := range v.Results {
					fmt.Fprintln(outFile, result.Link)
				}

				return nil
			}(); err != nil {
				if errors.Is(err, ErrRateLimitExceeded) {
					break
				}
				log.Println(err)
			}
		}

		fmt.Fprintf(rw, "Done. See %q.", outFile.Name())
	})

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	log.Fatal(http.ListenAndServe(addr, nil))
}
