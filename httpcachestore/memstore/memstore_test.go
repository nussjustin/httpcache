package memstore_test

import (
	"net/http"
	"net/url"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nussjustin/httpcache"
	"github.com/nussjustin/httpcache/httpcachestore/memstore"
	"github.com/nussjustin/httpcache/httpcachetest"
)

func TestMemoryStore(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{
				SupportsCacheGroups: true,
			},
			func() httpcache.Store {
				return memstore.New(memstore.Options{})
			},
		)
	})

	t.Run("ReturnDetailString", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{
				SupportsCacheGroups: true,
			},
			func() httpcache.Store {
				return memstore.New(memstore.Options{
					ReturnDetailString: true,
				})
			},
		)
	})

	t.Run("ReturnsNilForExpiredResponses", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{
				ReturnsNilForExpiredResponses: true,
				SupportsCacheGroups:           true,
			},
			func() httpcache.Store {
				return memstore.New(memstore.Options{
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
				SupportsCacheGroups:                true,
			},
			func() httpcache.Store {
				return memstore.New(memstore.Options{
					ReturnNilForExpiredResponses:      true,
					ReturnNilForExpiredResponsesAfter: 30 * time.Second,
				})
			},
		)
	})

	t.Run("ValidatesNoVarySearch", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{
				SupportsCacheGroups:   true,
				ValidatesNoVarySearch: true,
			},
			func() httpcache.Store {
				return memstore.New(memstore.Options{
					ValidateNoVarySearch: true,
				})
			},
		)
	})
}

func TestMemoryStore_Clear(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var s memstore.Store

		baseURL, _ := url.Parse("https://example.com")

		freshReq := httpcache.RequestData{
			Time:   time.Now(),
			Method: "GET",
			URL:    *baseURL.JoinPath("fresh"),
		}
		freshResp := httpcache.ResponseData{
			Time:       time.Now(),
			Status:     http.StatusText(http.StatusOK),
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": []string{"public, max-age=120"}},
		}

		if _, err := s.Set(t.Context(), freshReq, freshResp); err != nil {
			t.Fatalf("failed to store fresh response: %s", err)
		}

		expiredReq := httpcache.RequestData{
			Time:   time.Now(),
			Method: "GET",
			URL:    *baseURL.JoinPath("expired"),
		}
		expiredResp := httpcache.ResponseData{
			Time:       time.Now(),
			Status:     http.StatusText(http.StatusOK),
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": []string{"public, max-age=60"}},
		}

		if _, err := s.Set(t.Context(), expiredReq, expiredResp); err != nil {
			t.Fatalf("failed to store expired response: %s", err)
		}

		time.Sleep(time.Minute)
		synctest.Wait()

		// Safety checks. Should not be necessary
		if resp, _, err := s.Get(t.Context(), freshReq); err != nil {
			t.Fatalf("failed to get fresh request: %s", err)
		} else if resp == nil {
			t.Error("Get() returned nil for fresh response before Clear(), expected non-nil response")
		}

		if resp, _, err := s.Get(t.Context(), expiredReq); err != nil {
			t.Fatalf("failed to get expired request: %s", err)
		} else if resp == nil {
			t.Error("Get() returned nil for expired response before Clear(), expected non-nil response")
		}

		s.Clear()

		if resp, _, err := s.Get(t.Context(), freshReq); resp != nil {
			t.Error("got fresh response after Clear()")
		} else if err != nil {
			t.Errorf("got error when getting fresh response after Clear(): %s", err)
		}

		if resp, _, err := s.Get(t.Context(), expiredReq); resp != nil {
			t.Error("got expired response after Clear()")
		} else if err != nil {
			t.Errorf("got error when getting expired response after Clear(): %s", err)
		}
	})
}

func TestMemoryStore_Prune(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var s memstore.Store

		baseURL, _ := url.Parse("https://example.com")

		freshReq := httpcache.RequestData{
			Time:   time.Now(),
			Method: "GET",
			URL:    *baseURL.JoinPath("fresh"),
		}
		freshResp := httpcache.ResponseData{
			Time:       time.Now(),
			Status:     http.StatusText(http.StatusOK),
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": []string{"public, max-age=120"}},
		}

		if _, err := s.Set(t.Context(), freshReq, freshResp); err != nil {
			t.Fatalf("failed to store fresh response: %s", err)
		}

		expiredReq := httpcache.RequestData{
			Time:   time.Now(),
			Method: "GET",
			URL:    *baseURL.JoinPath("expired"),
		}
		expiredResp := httpcache.ResponseData{
			Time:       time.Now(),
			Status:     http.StatusText(http.StatusOK),
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": []string{"public, max-age=60"}},
		}

		if _, err := s.Set(t.Context(), expiredReq, expiredResp); err != nil {
			t.Fatalf("failed to store expired response: %s", err)
		}

		time.Sleep(time.Minute)
		synctest.Wait()

		// Safety checks. Should not be necessary
		if resp, _, err := s.Get(t.Context(), freshReq); err != nil {
			t.Fatalf("failed to get fresh request: %s", err)
		} else if resp == nil {
			t.Error("Get() returned nil for fresh response before Prune(), expected non-nil response")
		}

		if resp, _, err := s.Get(t.Context(), expiredReq); err != nil {
			t.Fatalf("failed to get expired request: %s", err)
		} else if resp == nil {
			t.Error("Get() returned nil for expired response before Prune(), expected non-nil response")
		}

		s.Prune()

		if resp, _, err := s.Get(t.Context(), freshReq); err != nil {
			t.Fatalf("failed to get fresh request after Prune(): %s", err)
		} else if resp == nil {
			t.Error("Get() returned nil for fresh response after Prune(), expected non-nil response")
		}

		if resp, _, err := s.Get(t.Context(), expiredReq); resp != nil {
			t.Error("got expired response after Prune()")
		} else if err != nil {
			t.Errorf("got error when getting expired response after Prune(): %s", err)
		}
	})
}
