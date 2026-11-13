package httpcachetest_test

import (
	"net/http"
	"net/url"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nussjustin/httpcache"
	"github.com/nussjustin/httpcache/httpcachetest"
)

func TestMemoryStore(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{},
			func() httpcache.Store {
				return httpcachetest.NewMemoryStore(httpcachetest.MemoryStoreOpts{})
			},
		)
	})

	t.Run("ReturnsNilForExpiredResponses", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{
				ReturnsNilForExpiredResponses: true,
			},
			func() httpcache.Store {
				return httpcachetest.NewMemoryStore(httpcachetest.MemoryStoreOpts{
					ReturnNilForExpiredResponses: true,
				})
			},
		)
	})

	t.Run("ReturnsNilForExpiredResponsesAfter", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{
				ReturnsNilForExpiredResponses:      true,
				ReturnsNilForExpiredResponsesAfter: 30 * time.Second,
			},
			func() httpcache.Store {
				return httpcachetest.NewMemoryStore(httpcachetest.MemoryStoreOpts{
					ReturnNilForExpiredResponses:      true,
					ReturnNilForExpiredResponsesAfter: 30 * time.Second,
				})
			},
		)
	})

	t.Run("ValidatesNoVarySearch", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{
				ValidatesNoVarySearch: true,
			},
			func() httpcache.Store {
				return httpcachetest.NewMemoryStore(httpcachetest.MemoryStoreOpts{
					ValidateNoVarySearch: true,
				})
			},
		)
	})
}

func TestMemoryStore_Clear(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var s httpcachetest.MemoryStore

		baseURL, _ := url.Parse("https://example.com")

		freshReq := &http.Request{Method: "GET", URL: baseURL.JoinPath("fresh")}
		freshResp := &http.Response{
			Status:     http.StatusText(http.StatusOK),
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": []string{"public, max-age=120"}},
		}

		if err := s.Set(t.Context(), freshReq, time.Now(), freshResp, time.Now()); err != nil {
			t.Fatalf("failed to store fresh response: %s", err)
		}

		expiredReq := &http.Request{Method: "GET", URL: baseURL.JoinPath("expired")}
		expiredResp := &http.Response{
			Status:     http.StatusText(http.StatusOK),
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": []string{"public, max-age=60"}},
		}

		if err := s.Set(t.Context(), expiredReq, time.Now(), expiredResp, time.Now()); err != nil {
			t.Fatalf("failed to store expired response: %s", err)
		}

		time.Sleep(time.Minute)

		// Safety checks. Should not be necessary
		if resp, err := s.Get(t.Context(), freshReq); err != nil { //nolint:bodyclose
			t.Fatalf("failed to get fresh request: %s", err)
		} else if resp == nil {
			t.Error("Get() returned nil for fresh response before Clear(), expected non-nil response")
		}

		if resp, err := s.Get(t.Context(), expiredReq); err != nil { //nolint:bodyclose
			t.Fatalf("failed to get expired request: %s", err)
		} else if resp == nil {
			t.Error("Get() returned nil for expired response before Clear(), expected non-nil response")
		}

		s.Clear()

		if resp, err := s.Get(t.Context(), freshReq); resp != nil { //nolint:bodyclose
			t.Error("got fresh response after Clear()")
		} else if err != nil {
			t.Errorf("got error when getting fresh response after Clear(): %s", err)
		}

		if resp, err := s.Get(t.Context(), expiredReq); resp != nil { //nolint:bodyclose
			t.Error("got expired response after Clear()")
		} else if err != nil {
			t.Errorf("got error when getting expired response after Clear(): %s", err)
		}
	})
}

func TestMemoryStore_Prune(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var s httpcachetest.MemoryStore

		baseURL, _ := url.Parse("https://example.com")

		freshReq := &http.Request{Method: "GET", URL: baseURL.JoinPath("fresh")}
		freshResp := &http.Response{
			Status:     http.StatusText(http.StatusOK),
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": []string{"public, max-age=120"}},
		}

		if err := s.Set(t.Context(), freshReq, time.Now(), freshResp, time.Now()); err != nil {
			t.Fatalf("failed to store fresh response: %s", err)
		}

		expiredReq := &http.Request{Method: "GET", URL: baseURL.JoinPath("expired")}
		expiredResp := &http.Response{
			Status:     http.StatusText(http.StatusOK),
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": []string{"public, max-age=60"}},
		}

		if err := s.Set(t.Context(), expiredReq, time.Now(), expiredResp, time.Now()); err != nil {
			t.Fatalf("failed to store expired response: %s", err)
		}

		time.Sleep(time.Minute)

		// Safety checks. Should not be necessary
		if resp, err := s.Get(t.Context(), freshReq); err != nil { //nolint:bodyclose
			t.Fatalf("failed to get fresh request: %s", err)
		} else if resp == nil {
			t.Error("Get() returned nil for fresh response before Prune(), expected non-nil response")
		}

		if resp, err := s.Get(t.Context(), expiredReq); err != nil { //nolint:bodyclose
			t.Fatalf("failed to get expired request: %s", err)
		} else if resp == nil {
			t.Error("Get() returned nil for expired response before Prune(), expected non-nil response")
		}

		s.Prune()

		if resp, err := s.Get(t.Context(), freshReq); err != nil { //nolint:bodyclose
			t.Fatalf("failed to get fresh request after Prune(): %s", err)
		} else if resp == nil {
			t.Error("Get() returned nil for fresh response after Prune(), expected non-nil response")
		}

		if resp, err := s.Get(t.Context(), expiredReq); resp != nil { //nolint:bodyclose
			t.Error("got expired response after Prune()")
		} else if err != nil {
			t.Errorf("got error when getting expired response after Prune(): %s", err)
		}
	})
}
