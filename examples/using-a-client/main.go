package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/nussjustin/httpcache"
	"github.com/nussjustin/httpcache/httpcachetest"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := httpcache.Client{
		Config: httpcache.Config{
			Name:           "my-awesome-app",
			AddCacheStatus: true,
		},
		Store: &httpcachetest.MemoryStore{},
	}

	req1, err := http.NewRequestWithContext(ctx, "GET", "https://example.com/", nil)
	if err != nil {
		panic(err)
	}

	resp1, err := client.Do(req1)
	if err != nil {
		panic(err)
	}
	drainBody(resp1)

	fmt.Println(resp1.Status)
	// Result: 200 OK

	fmt.Println(resp1.Header.Get("Cache-Status"))
	// Result: "my-awesome-app";fwd=miss;stored

	req2, err := http.NewRequestWithContext(ctx, "GET", "https://example.com/", nil)
	if err != nil {
		panic(err)
	}
	req2.Header.Set("Cache-Control", "public, max-stale=60")

	resp2, err := client.Do(req2)
	if err != nil {
		panic(err)
	}
	drainBody(resp2)

	fmt.Println(resp2.Status)
	// Result: 200 OK

	fmt.Println(resp2.Header.Get("Cache-Status"))
	// Result: "my-awesome-app";fwd=stale;ttl=0
}

func drainBody(resp *http.Response) {
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		panic(err)
	}

	if err := resp.Body.Close(); err != nil {
		panic(err)
	}
}
