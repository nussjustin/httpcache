package httpcache_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unsafe"

	"github.com/google/go-cmp/cmp"

	"github.com/nussjustin/httpcache"
	"github.com/nussjustin/httpcache/httpcachestore/memstore"
)

func headerEqual(a, b http.Header) bool {
	if len(a) != len(b) {
		return false
	}

	for k := range a {
		if !slices.Equal(a[k], b[k]) {
			return false
		}
	}

	return true
}

type errReader struct{}

func (r errReader) Read([]byte) (n int, err error) {
	return 0, errors.New("read error")
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

// Do implements the [github.com/nussjustin/httpcache.HTTPClient] interface.
func (f roundTripperFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type testStore struct {
	wrapped           httpcache.Store
	failOnGet         bool
	failOnStore       bool
	invalidated       []string
	invalidatedGroups []string
	stored            int
}

var errStoreFail = errors.New("fail on store")

func (t *testStore) Get(ctx context.Context, req *httpcache.RequestData) (resp *httpcache.ResponseData, detail string, err error) {
	if t.failOnGet {
		return nil, "fail-on-get", errStoreFail
	}
	resp, _, err = t.wrapped.Get(ctx, req)
	if resp != nil {
		detail = "hit"
	} else {
		detail = "miss"
	}
	return
}

func (t *testStore) Invalidate(ctx context.Context, urls ...url.URL) error {
	for _, u := range urls {
		t.invalidated = append(t.invalidated, u.String())
	}
	return t.wrapped.Invalidate(ctx, urls...)
}

func (t *testStore) InvalidateGroups(ctx context.Context, groups ...string) error {
	t.invalidatedGroups = append(t.invalidatedGroups, groups...)
	return t.wrapped.InvalidateGroups(ctx, groups...)
}

func (t *testStore) Set(ctx context.Context, req *httpcache.RequestData, resp *httpcache.ResponseData) (detail string, err error) {
	t.stored++
	if t.failOnStore {
		return "fail-on-store", errStoreFail
	}
	return t.wrapped.Set(ctx, req, resp)
}

type reqOpt func(*http.Request)

func withReqBody(r io.Reader) reqOpt {
	return func(req *http.Request) {
		rc, ok := r.(io.ReadCloser)
		if !ok {
			rc = io.NopCloser(r)
		}
		req.Body = rc
	}
}

func withReqHeader(name, value string) reqOpt {
	return func(req *http.Request) {
		req.Header.Add(name, value)
	}
}

func withReqMethod(method string) reqOpt {
	return func(req *http.Request) {
		req.Method = method
	}
}

func withReqUrl(urlStr string) reqOpt {
	return func(req *http.Request) {
		parsedURL, err := url.Parse(urlStr)
		if err != nil {
			panic(err)
		}
		req.URL = parsedURL
	}
}

func newReq(opts ...reqOpt) *http.Request {
	req := &http.Request{Header: make(http.Header)}

	opts = append(
		[]reqOpt{
			withReqMethod("GET"),
			withReqUrl("http://example.com/"),
		},
		opts...,
	)

	for _, opt := range opts {
		opt(req)
	}

	return req
}

type respOpt func(*http.Response)

func withRespBody(r io.Reader) respOpt {
	return func(resp *http.Response) {
		resp.Body = io.NopCloser(r)
	}
}

func withRespHeader(name, value string) respOpt {
	return func(resp *http.Response) {
		resp.Header.Add(name, value)
	}
}

func withRespStatus(code int) respOpt {
	return func(resp *http.Response) {
		resp.Status = http.StatusText(code)
		resp.StatusCode = code
	}
}

// bodyclose:handled
func newResp(opts ...respOpt) *http.Response {
	resp := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}

	opts = append(
		[]respOpt{
			withRespStatus(http.StatusOK),
		},
		opts...,
	)

	for _, opt := range opts {
		opt(resp)
	}

	return resp
}

func diffReader(tb testing.TB, r1 io.Reader, r2 io.Reader) string {
	var b1, b2 []byte
	var err error

	if r1 != nil {
		if b1, err = io.ReadAll(r1); err != nil {
			tb.Fatalf("failed to read from r1: %s", err)
		}
	}

	if r2 != nil {
		if b2, err = io.ReadAll(r2); err != nil {
			tb.Fatalf("failed to read from r2: %s", err)
		}
	}

	return cmp.Diff(string(b1), string(b2))
}

func TestClient_Do(t *testing.T) {
	t.Run("without cache status", func(t *testing.T) {
		testClientDo(t, false)
	})

	t.Run("with cache status", func(t *testing.T) {
		testClientDo(t, true)
	})
}

func testClientDo(t *testing.T, withCacheStatus bool) {
	type transaction struct {
		req                   *http.Request
		resp                  *http.Response
		respErr               error
		failOnGet             bool
		failOnStore           bool
		wantCacheStatus       httpcache.Status
		wantInvalidated       []string
		wantInvalidatedGroups []string
		wantStored            int
		wantReq               *http.Request
		wantResp              *http.Response
		wantRespErr           bool
	}

	tests := []struct {
		name   string
		config httpcache.Config
		txs    []transaction
	}{
		{
			name: "fresh cached response",
			txs: []transaction{
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(),
					wantCacheStatus: httpcache.Status{
						Cache:  "my-cache",
						Detail: "hit",
						Hit:    true,
						TTL:    time.Minute,
					},
					wantReq: newReq(),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "expired response",
			txs: []transaction{
				{
					req:             newReq(),
					resp:            newResp(withRespHeader("Cache-Control", "public, max-age=60")),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantStored:      1,
					wantReq:         newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req:             newReq(),
					resp:            newResp(withRespHeader("Cache-Control", "public, max-age=60")),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantStored:      1,
					wantReq:         newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Transaction-Id", "1")),
				},
			},
		},
		{
			name: "stale cached response validated by etag",
			txs: []transaction{
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my tag"`)),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my tag"`),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-None-Match", `"my tag"`)),
					resp: newResp(withRespStatus(http.StatusNotModified)),
					wantCacheStatus: httpcache.Status{
						Cache:           "my-cache",
						Detail:          "hit",
						Forwarded:       httpcache.ForwardedReasonStale,
						ForwardedStatus: http.StatusNotModified,
					},
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-None-Match", `"my tag"`)),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my tag"`),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "stale cached response validated by weak etag",
			txs: []transaction{
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `W/"my tag"`)),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `W/"my tag"`),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-None-Match", `"my tag"`)),
					resp: newResp(withRespStatus(http.StatusNotModified)),
					wantCacheStatus: httpcache.Status{
						Cache:           "my-cache",
						Detail:          "hit",
						Forwarded:       httpcache.ForwardedReasonStale,
						ForwardedStatus: http.StatusNotModified,
					},
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-None-Match", `"my tag"`)),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `W/"my tag"`),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "failed validation by etag",
			txs: []transaction{
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my tag"`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my tag"`),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-None-Match", `"my tag"`)),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"other tag"`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonStale, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-None-Match", `"my tag"`)),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"other tag"`),
						withRespHeader("Transaction-Id", "1")),
				},
			},
		},
		{
			name: "stale cached response validated by last-modified",
			txs: []transaction{
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					resp: newResp(withRespStatus(http.StatusNotModified)),
					wantCacheStatus: httpcache.Status{
						Cache:           "my-cache",
						Detail:          "hit",
						Forwarded:       httpcache.ForwardedReasonStale,
						ForwardedStatus: http.StatusNotModified,
					},
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "failed validation by last-modified",
			txs: []transaction{
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Last-Modified", `Mon, 03 Jan 2006 15:04:05 GMT`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonStale, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Last-Modified", `Mon, 03 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "1")),
				},
			},
		},
		{
			name: "stale cached response validated by etag and last-modified",
			txs: []transaction{
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withReqHeader("If-None-Match", `"my etag"`)),
					resp: newResp(withRespStatus(http.StatusNotModified)),
					wantCacheStatus: httpcache.Status{
						Cache:           "my-cache",
						Detail:          "hit",
						Forwarded:       httpcache.ForwardedReasonStale,
						ForwardedStatus: http.StatusNotModified,
					},
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withReqHeader("If-None-Match", `"my etag"`)),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "failed validation by etag only",
			txs: []transaction{
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withReqHeader("If-None-Match", `"my etag"`)),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"other etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonStale, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withReqHeader("If-None-Match", `"my etag"`)),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"other etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "1")),
				},
			},
		},
		{
			name: "failed validation by last-modified only",
			txs: []transaction{
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withReqHeader("If-None-Match", `"my etag"`)),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 03 Jan 2006 15:04:05 GMT`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonStale, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withReqHeader("If-None-Match", `"my etag"`)),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 03 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "1")),
				},
			},
		},
		{
			name: "failed validation by etag and last-modified",
			txs: []transaction{
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withReqHeader("If-None-Match", `"my etag"`)),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"other etag"`),
						withRespHeader("Last-Modified", `Mon, 03 Jan 2006 15:04:05 GMT`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonStale, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-Modified-Since", `Mon, 02 Jan 2006 15:04:05 GMT`),
						withReqHeader("If-None-Match", `"my etag"`)),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"other etag"`),
						withRespHeader("Last-Modified", `Mon, 03 Jan 2006 15:04:05 GMT`),
						withRespHeader("Transaction-Id", "1")),
				},
			},
		},
		{
			name: "only if cached",
			txs: []transaction{
				{
					req: newReq(withReqHeader("Cache-Control", "only-if-cached")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Detail:    "miss",
						Forwarded: httpcache.ForwardedReasonMiss,
					},
					wantResp: newResp(
						withRespStatus(http.StatusGatewayTimeout)),
				},
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "1")),
				},
				{
					req: newReq(withReqHeader("Cache-Control", "only-if-cached")),
					wantCacheStatus: httpcache.Status{
						Cache:  "my-cache",
						Detail: "hit",
						Hit:    true,
						TTL:    time.Minute,
					},
					wantReq: newReq(),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "1")),
				},
			},
		},
		{
			name: "no-store response",
			txs: []transaction{
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120, no-store")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Detail:    "miss",
						Forwarded: httpcache.ForwardedReasonMiss,
					},
					wantStored: 0,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120, no-store"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "uncacheable headers removed",
			txs: []transaction{
				{
					req: newReq(),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Connection", "close"),
						withRespHeader("Proxy-Authenticate", "Basis")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Connection", "close"),
						withRespHeader("Proxy-Authenticate", "Basis"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(),
					wantCacheStatus: httpcache.Status{
						Cache:  "my-cache",
						Detail: "hit",
						Hit:    true,
						TTL:    time.Minute,
					},
					wantReq: newReq(),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "unsupported method",
			txs: []transaction{
				{
					req:             newReq(withReqMethod("POST")),
					resp:            newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMethod},
					wantInvalidated: []string{"http://example.com/"},
					wantStored:      0,
					wantReq:         newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "custom supported method",
			config: httpcache.Config{
				SupportedRequestMethods: []string{"POST"},
			},
			txs: []transaction{
				{
					req:             newReq(withReqMethod("POST")),
					resp:            newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantInvalidated: []string{"http://example.com/"},
					wantStored:      1,
					wantReq:         newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "request with body",
			txs: []transaction{
				{
					req: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("first request body"))),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=240")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("first request body"))),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=240"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("second request body"))),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("second request body"))),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "1")),
				},
				{
					req: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("second request body"))),
					wantCacheStatus: httpcache.Status{
						Cache:  "my-cache",
						Detail: "hit",
						Hit:    true,
						TTL:    time.Minute,
					},
					wantReq: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("first request body"))),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "1")),
				},
				{
					req: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("first request body"))),
					wantCacheStatus: httpcache.Status{
						Cache:  "my-cache",
						Detail: "hit",
						Hit:    true,
						TTL:    time.Minute,
					},
					wantReq: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("first request body"))),
					wantResp: newResp(
						withRespHeader("Age", "180"),
						withRespHeader("Cache-Control", "public, max-age=240"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "request with body on ignored method",
			config: httpcache.Config{
				RequestMethodsWithoutBody: []string{"QUERY"},
			},
			txs: []transaction{
				{
					req: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("first request body"))),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=240")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("first request body"))),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=240"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("first request body"))),
					wantCacheStatus: httpcache.Status{
						Cache:  "my-cache",
						Detail: "hit",
						Hit:    true,
						TTL:    3 * time.Minute,
					},
					wantReq: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("first request body"))),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public, max-age=240"),
						withRespHeader("Transaction-Id", "0")),
				},
				// Different body, but response should still come from cache
				{
					req: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("second request body"))),
					wantCacheStatus: httpcache.Status{
						Cache:  "my-cache",
						Detail: "hit",
						Hit:    true,
						TTL:    2 * time.Minute,
					},
					wantReq: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("second request body"))),
					wantResp: newResp(
						withRespHeader("Age", "120"),
						withRespHeader("Cache-Control", "public, max-age=240"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "request with body on default ignored method",
			txs: []transaction{
				{
					req: newReq(
						withReqBody(strings.NewReader("first request body"))),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=240")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqBody(strings.NewReader("first request body"))),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=240"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqBody(strings.NewReader("first request body"))),
					wantCacheStatus: httpcache.Status{
						Cache:  "my-cache",
						Detail: "hit",
						Hit:    true,
						TTL:    3 * time.Minute,
					},
					wantReq: newReq(
						withReqBody(strings.NewReader("first request body"))),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public, max-age=240"),
						withRespHeader("Transaction-Id", "0")),
				},
				// Different body, but response should still come from cache
				{
					req: newReq(
						withReqBody(strings.NewReader("second request body"))),
					wantCacheStatus: httpcache.Status{
						Cache:  "my-cache",
						Detail: "hit",
						Hit:    true,
						TTL:    2 * time.Minute,
					},
					wantReq: newReq(
						withReqBody(strings.NewReader("second request body"))),
					wantResp: newResp(
						withRespHeader("Age", "120"),
						withRespHeader("Cache-Control", "public, max-age=240"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "expect header set",
			txs: []transaction{
				{
					req:             newReq(withReqHeader("Expect", "")),
					resp:            newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonRequest},
					wantStored:      0,
					wantReq:         newReq(withReqHeader("Expect", "")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "request error",
			txs: []transaction{
				{
					req:         newReq(),
					respErr:     errors.New("test error"),
					wantStored:  0,
					wantReq:     newReq(),
					wantRespErr: true,
				},
			},
		},
		{
			name: "request error on validation",
			txs: []transaction{
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my tag"`)),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantStored:      1,
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my tag"`),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-None-Match", `"my tag"`)),
					respErr: errors.New("test error"),
					wantReq: newReq(
						withReqHeader("Cache-Control", "max-stale=30"),
						withReqHeader("If-None-Match", `"my tag"`)),
					wantRespErr: true,
				},
			},
		},
		{
			name: "error when retrieving from store",
			txs: []transaction{
				{
					req:             newReq(),
					resp:            newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantStored:      1,
					wantReq:         newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req:             newReq(),
					resp:            newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					failOnGet:       true,
					wantCacheStatus: httpcache.Status{Cache: "my-cache", Forwarded: httpcache.ForwardedReasonMiss, Stored: true},
					wantStored:      1,
					wantReq:         newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "1")),
				},
			},
		},
		{
			name: "error when storing",
			txs: []transaction{
				{
					req:         newReq(),
					resp:        newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					failOnStore: true,
					wantStored:  1,
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Detail:    "miss",
						Forwarded: httpcache.ForwardedReasonMiss,
					},
					wantReq: newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "error when reading request body",
			txs: []transaction{
				{
					req:         newReq(withReqMethod("QUERY"), withReqBody(errReader{})),
					resp:        newResp(),
					wantReq:     newReq(withReqMethod("QUERY"), withReqBody(errReader{})),
					wantResp:    newResp(),
					wantRespErr: true,
				},
			},
		},
		{
			name: "error when reading response body",
			txs: []transaction{
				{
					req:         newReq(),
					resp:        newResp(withRespBody(errReader{})),
					wantReq:     newReq(),
					wantResp:    newResp(withRespBody(errReader{})),
					wantRespErr: true,
				},
			},
		},
		{
			name:   "fresh cached response with heuristic freshness lifetime",
			config: httpcache.Config{HeuristicFreshnessLifetime: 2 * time.Minute},
			txs: []transaction{
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(),
					wantCacheStatus: httpcache.Status{
						Cache:  "my-cache",
						Detail: "hit",
						Hit:    true,
						TTL:    time.Minute,
					},
					wantReq: newReq(),
					wantResp: newResp(
						withRespHeader("Age", "60"),
						withRespHeader("Cache-Control", "public"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "response invalidated",
			txs: []transaction{
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req:  newReq(withReqMethod("POST")),
					resp: newResp(),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMethod,
					},
					wantInvalidated: []string{"http://example.com/"},
					wantReq:         newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespHeader("Transaction-Id", "1")),
				},
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "2")),
				},
			},
		},
		{
			name: "response invalidated for custom unsafe method",
			config: httpcache.Config{IsSafeMethodFunc: func(string) bool {
				return false
			}},
			txs: []transaction{
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantInvalidated: []string{"http://example.com/"},
					wantStored:      1,
					wantReq:         newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "response invalidated with content-location and location",
			txs: []transaction{
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(withReqMethod("POST")),
					resp: newResp(
						withRespHeader("Content-Location", "http://example.com/content-location-1"),
						withRespHeader("Content-Location", "http://example.com/content-location-2"),
						withRespHeader("Location", "http://example.com/location-1"),
						withRespHeader("Location", "http://example.com/location-2")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMethod,
					},
					wantInvalidated: []string{
						"http://example.com/",
						"http://example.com/content-location-1",
						"http://example.com/content-location-2",
						"http://example.com/location-1",
						"http://example.com/location-2",
					},
					wantReq: newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespHeader("Content-Location", "http://example.com/content-location-1"),
						withRespHeader("Content-Location", "http://example.com/content-location-2"),
						withRespHeader("Location", "http://example.com/location-1"),
						withRespHeader("Location", "http://example.com/location-2"),
						withRespHeader("Transaction-Id", "1")),
				},
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "2")),
				},
			},
		},
		{
			name: "response invalidation skips invalid content-location and location",
			txs: []transaction{
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(withReqMethod("POST")),
					resp: newResp(
						withRespHeader("Content-Location", "invalid\x00"),
						withRespHeader("Content-Location", "http://example.com/content-location-2"),
						withRespHeader("Content-Location", "http://example.com:99999/content-location-3"),
						withRespHeader("Location", "invalid\x00"),
						withRespHeader("Location", "http://example.com/location-2"),
						withRespHeader("Location", "http://example.com:99999/location-2")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMethod,
					},
					wantInvalidated: []string{
						"http://example.com/",
						"http://example.com/content-location-2",
						"http://example.com/location-2",
					},
					wantReq: newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespHeader("Content-Location", "invalid\x00"),
						withRespHeader("Content-Location", "http://example.com/content-location-2"),
						withRespHeader("Content-Location", "http://example.com:99999/content-location-3"),
						withRespHeader("Location", "invalid\x00"),
						withRespHeader("Location", "http://example.com/location-2"),
						withRespHeader("Location", "http://example.com:99999/location-2"),
						withRespHeader("Transaction-Id", "1")),
				},
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "2")),
				},
			},
		},
		{
			name: "response invalidation ignores content-location and location with other origins",
			txs: []transaction{
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(withReqMethod("POST")),
					resp: newResp(
						withRespHeader("Content-Location", "http://example.org/content-location-1"),
						withRespHeader("Location", "https://example.com/location-1"),
						withRespHeader("Location", "http://example.com:80/location-2"),
						withRespHeader("Location", "http://example.com:1080/location-2")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMethod,
					},
					wantInvalidated: []string{
						"http://example.com/",
						"http://example.com:80/location-2",
					},
					wantReq: newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespHeader("Content-Location", "http://example.org/content-location-1"),
						withRespHeader("Location", "https://example.com/location-1"),
						withRespHeader("Location", "http://example.com:80/location-2"),
						withRespHeader("Location", "http://example.com:1080/location-2"),
						withRespHeader("Transaction-Id", "1")),
				},
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "2")),
				},
			},
		},
		{
			name: "response not invalidated on error",
			txs: []transaction{
				{
					req:  newReq(),
					resp: newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req: newReq(withReqMethod("POST")),
					resp: newResp(
						withRespStatus(http.StatusNotFound)),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMethod,
					},
					wantReq: newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespStatus(http.StatusNotFound),
						withRespHeader("Transaction-Id", "1")),
				},
			},
		},
		{
			name: "response invalidated for cache groups",
			txs: []transaction{
				{
					req: newReq(withReqMethod("POST")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Cache-Group-Invalidation", `"group-a";parameter=ignored, "group-b"`)),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMethod,
					},
					wantInvalidated:       []string{"http://example.com/"},
					wantInvalidatedGroups: []string{"group-a", "group-b"},
					wantReq:               newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Cache-Group-Invalidation", `"group-a";parameter=ignored, "group-b"`),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "response invalidated for cache groups and custom unsafe method",
			config: httpcache.Config{IsSafeMethodFunc: func(string) bool {
				return false
			}},
			txs: []transaction{
				{
					req: newReq(),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Cache-Group-Invalidation", `"group-a", "group-b"`)),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantInvalidated:       []string{"http://example.com/"},
					wantInvalidatedGroups: []string{"group-a", "group-b"},
					wantStored:            1,
					wantReq:               newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Cache-Group-Invalidation", `"group-a", "group-b"`),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "response not invalidated for invalid cache group header",
			txs: []transaction{
				{
					req: newReq(withReqMethod("POST")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Cache-Group-Invalidation", `("list")`)),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMethod,
					},
					wantInvalidated: []string{"http://example.com/"},
					wantReq:         newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Cache-Group-Invalidation", `("list")`),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "response not invalidated for invalid cache groups",
			txs: []transaction{
				{
					req: newReq(withReqMethod("POST")),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Cache-Group-Invalidation", `"valid-1", invalid, "valid-2"`)),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMethod,
					},
					wantInvalidated: []string{"http://example.com/"},
					wantReq:         newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Cache-Group-Invalidation", `"valid-1", invalid, "valid-2"`),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		// Tests for read optimizations
		{
			name: "body from bytes.Buffer",
			txs: []transaction{
				{
					req: newReq(
						withReqMethod("QUERY"),
						withReqBody(bytes.NewBufferString("request body"))),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespBody(bytes.NewBufferString("response body"))),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqMethod("QUERY"),
						withReqBody(bytes.NewBufferString("request body"))),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespBody(bytes.NewBufferString("response body")),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "body from bytes.Reader",
			txs: []transaction{
				{
					req: newReq(
						withReqMethod("QUERY"),
						withReqBody(bytes.NewReader([]byte("request body")))),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespBody(bytes.NewReader([]byte("response body")))),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqMethod("QUERY"),
						withReqBody(bytes.NewReader([]byte("request body")))),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespBody(bytes.NewReader([]byte("response body"))),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "body from strings.Reader",
			txs: []transaction{
				{
					req: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("request body"))),
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespBody(strings.NewReader("response body"))),
					wantCacheStatus: httpcache.Status{
						Cache:     "my-cache",
						Forwarded: httpcache.ForwardedReasonMiss,
						Stored:    true,
					},
					wantStored: 1,
					wantReq: newReq(
						withReqMethod("QUERY"),
						withReqBody(strings.NewReader("request body"))),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespBody(strings.NewReader("response body")),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tt.config.AddCacheStatus = withCacheStatus
				tt.config.Name = "my-cache"

				var sharedStore memstore.Store

				for i, tx := range tt.txs {
					if tx.resp != nil {
						tx.resp.Header.Set("Transaction-Id", strconv.Itoa(i))
					}

					store := &testStore{
						wrapped:     &sharedStore,
						failOnGet:   tx.failOnGet,
						failOnStore: tx.failOnStore,
					}

					resp, err := (&httpcache.Client{
						Config: tt.config,
						Store:  store,
						HTTPClient: &http.Client{
							Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
								if got, want := req.Method, tx.wantReq.Method; got != want {
									t.Errorf("Tx %d: Do() Request.Method = %s, want %s", i, got, want)
								}

								if got, want := req.Header, tx.wantReq.Header; !headerEqual(got, want) {
									t.Errorf("Tx %d: Do() Request.Header = %#v, want %#v", i, got, want)
								}

								if diff := diffReader(t, tx.wantReq.Body, req.Body); diff != "" {
									t.Errorf("Tx %d: Do() Request.Body mismatch (-want +got):\n%s", i, diff)
								}

								if tx.respErr != nil {
									return nil, tx.respErr
								}

								if tx.resp == nil {
									t.Fatalf("Tx %d: no response defined for transaction", i)
								}

								tx.resp.Request = req

								return tx.resp, nil
							}),
						},
					}).Do(tx.req)

					if resp != nil && resp.Body != nil {
						_ = resp.Body.Close()
					}

					if (err != nil) != tx.wantRespErr {
						t.Errorf("Tx %d: Do() error = %v, wantErr = %v", i, err, tx.wantRespErr)
						return
					}

					if !tx.wantRespErr {
						if got, want := resp.StatusCode, tx.wantResp.StatusCode; got != want {
							t.Errorf("Tx %d: Do() Response.StatusCode = %d, want %d", i, got, want)
						}

						wantRespHeader := tx.wantResp.Header

						if withCacheStatus && tx.wantCacheStatus != (httpcache.Status{}) {
							wantCacheStatus, err := tx.wantCacheStatus.AppendText(nil)
							if err != nil {
								t.Fatal(err)
							}

							wantRespHeader = wantRespHeader.Clone()
							wantRespHeader.Add("Cache-Status", string(wantCacheStatus))
						}

						if got, want := resp.Header, wantRespHeader; want != nil && !headerEqual(got, want) {
							t.Errorf("Tx %d: Do() Response.Header = %#v, want %#v", i, got, want)
						}

						if diff := diffReader(t, tx.wantResp.Body, resp.Body); diff != "" {
							t.Errorf("Tx %d: Do() Response.Body mismatch (-want +got):\n%s", i, diff)
						}

						if got, want := store.invalidated, tx.wantInvalidated; !slices.Equal(got, want) {
							t.Errorf("Tx %d: Do() got Store.Invalidate calls = %v, want %v", i, got, want)
						}

						if got, want := store.stored, tx.wantStored; got != want {
							t.Errorf("Tx %d: Do() got Store.Set calls = %v, want %v", i, got, want)
						}
					}

					synctest.Sleep(time.Minute)
				}
			})
		})
	}
}

type dummyStore struct {
	resp *httpcache.ResponseData
}

var _ httpcache.Store = (*dummyStore)(nil)

func (s *dummyStore) Get(_ context.Context, _ *httpcache.RequestData) (*httpcache.ResponseData, string, error) {
	return s.resp, "", nil
}

func (s *dummyStore) Invalidate(_ context.Context, _ ...url.URL) error {
	return nil
}

func (s *dummyStore) InvalidateGroups(_ context.Context, _ ...string) error {
	return nil
}

func (s *dummyStore) Set(_ context.Context, _ *httpcache.RequestData, _ *httpcache.ResponseData) (string, error) {
	return "", nil
}

func BenchmarkClient_Do(b *testing.B) {
	b.Run("cache hit", func(b *testing.B) {
		req := newReq(
			withReqMethod("QUERY"),
			withReqHeader("Cache-Control", "no-cache"),
			withReqBody(strings.NewReader("request body")))

		store := &dummyStore{
			resp: &httpcache.ResponseData{
				Time:       time.Now(),
				Status:     http.StatusText(http.StatusOK),
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"public, max-age=86400"},
				},
				Body: []byte("response body"),
			},
		}

		client := &httpcache.Client{
			Config: httpcache.Config{
				Name:           "my-cache",
				AddCacheStatus: true,
			},
			Store: store,
			HTTPClient: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				panic("request not served from cache")
			}),
		}

		b.ReportAllocs()

		for b.Loop() {
			resp, err := client.Do(req)
			if err != nil {
				b.Fatal(err)
			}
			_ = resp.Body.Close()
		}
	})

	b.Run("cache miss", func(b *testing.B) {
		req := newReq(
			withReqMethod("QUERY"),
			withReqHeader("Cache-Control", "no-cache"),
			withReqBody(strings.NewReader("request body")))

		resp := newResp(
			withRespHeader("Cache-Control", "public, max-age=120"),
			withRespBody(strings.NewReader("response body")))

		store := &dummyStore{}

		client := &httpcache.Client{
			Config: httpcache.Config{
				Name:           "my-cache",
				AddCacheStatus: true,
			},
			Store: store,
			HTTPClient: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return resp, nil
			}),
		}

		b.ReportAllocs()

		for b.Loop() {
			resp, err := client.Do(req)
			if err != nil {
				b.Fatal(err)
			}
			_ = resp.Body.Close()
		}
	})

	b.Run("uncacheable request", func(b *testing.B) {
		req := newReq(
			withReqMethod("QUERY"),
			withReqHeader("Cache-Control", "no-cache"),
			withReqBody(strings.NewReader("request body")))

		resp := newResp(
			withRespHeader("Cache-Control", "public, max-age=120"),
			withRespBody(strings.NewReader("response body")))

		store := &testStore{
			failOnGet:   true,
			failOnStore: true,
		}

		client := &httpcache.Client{
			Config: httpcache.Config{
				Name:                           "my-cache",
				AddCacheStatus:                 true,
				RespectRequestDirectiveNoCache: true,
			},
			Store: store,
			HTTPClient: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return resp, nil
			}),
		}

		b.ReportAllocs()

		for b.Loop() {
			resp, err := client.Do(req)
			if err != nil {
				b.Fatal(err)
			}
			_ = resp.Body.Close()

			if store.stored != 0 {
				b.Fatalf("unexpected store calls: got %d, want 0", store.stored)
			}
		}
	})

	b.Run("uncacheable response", func(b *testing.B) {
		req := newReq(
			withReqMethod("QUERY"),
			withReqHeader("Cache-Control", "no-cache"),
			withReqBody(strings.NewReader("request body")))

		resp := newResp(
			withRespHeader("Cache-Control", "private"),
			withRespBody(strings.NewReader("response body")))

		store := &testStore{
			failOnGet:   true,
			failOnStore: true,
		}

		client := &httpcache.Client{
			Config: httpcache.Config{
				Name:           "my-cache",
				AddCacheStatus: true,
			},
			Store: store,
			HTTPClient: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return resp, nil
			}),
		}

		b.ReportAllocs()

		for b.Loop() {
			resp, err := client.Do(req)
			if err != nil {
				b.Fatal(err)
			}
			_ = resp.Body.Close()

			if store.stored != 0 {
				b.Fatalf("unexpected store calls: got %d, want 0", store.stored)
			}
		}
	})
}

func mapsIdentical[MX, MY ~map[K]V, K comparable, V any](x MX, y MY) bool {
	// Maps in Go are references yet the core language
	// provides no safe way to ask whether they alias.
	type pointer = unsafe.Pointer
	return *(*pointer)(pointer(&x)) == *(*pointer)(pointer(&y))
}

func slicesIdentical[S ~[]V, V any](x S, y S) bool {
	return unsafe.SliceData(x) == unsafe.SliceData(y)
}

func TestRequestData_Clone(t *testing.T) {
	orig := &httpcache.RequestData{
		Time:   time.Now(),
		Method: "GET",
		URL:    &url.URL{Scheme: "https", Host: "example.com", Path: "/", RawQuery: "a=1&b=2"},
		Header: http.Header{
			"Header-1": []string{"Value-1"},
			"Header-2": []string{"Value-2", "Value-3"},
		},
		Body: []byte("request body"),
		ParsedDirectives: httpcache.RequestDirectives{
			NoCache: true,
			NoStore: true,
			Extensions: httpcache.ExtensionDirectives{
				{Name: "Extension-1"},
				{Name: "Extension-2", Value: httpcache.Opt[string]{Value: "Value", Valid: true}},
			},
		},
	}

	cloned := orig.Clone()

	if diff := cmp.Diff(orig, cloned); diff != "" {
		t.Errorf("RequestData.Clone() mismatch (-want +got):\n%s", diff)
	}

	if orig.URL == cloned.URL {
		t.Errorf("RequestData.Clone() returned same *url.URL")
	}

	if mapsIdentical(orig.Header, cloned.Header) {
		t.Errorf("RequestData.Clone() returned same Header map")
	}

	for k := range orig.Header {
		if got, want := orig.Header[k], cloned.Header[k]; slicesIdentical(got, want) {
			t.Errorf("RequestData.Clone() returned same Header %q slice", k)
		}
	}

	if slicesIdentical(orig.ParsedDirectives.Extensions, cloned.ParsedDirectives.Extensions) {
		t.Errorf("RequestData.Clone() returned same ExtensionDirectives slice")
	}
}

func TestResponseData_Age(t *testing.T) {
	tests := []struct {
		name string
		resp func(time.Time) httpcache.ResponseData
		want time.Duration
	}{
		{
			name: "no Date header",
			resp: func(time.Time) httpcache.ResponseData {
				return httpcache.ResponseData{}
			},
			want: time.Minute,
		},
		{
			name: "single Date header",
			resp: func(now time.Time) httpcache.ResponseData {
				return httpcache.ResponseData{
					Header: http.Header{
						"Date": []string{now.Add(-5 * time.Minute).UTC().Format(http.TimeFormat)},
					},
				}
			},
			want: 6 * time.Minute,
		},
		{
			name: "invalid Date header",
			resp: func(time.Time) httpcache.ResponseData {
				return httpcache.ResponseData{
					Header: http.Header{
						"Date": []string{"invalid"},
					},
				}
			},
			want: time.Minute,
		},
		{
			name: "multiple Date headers",
			resp: func(now time.Time) httpcache.ResponseData {
				return httpcache.ResponseData{
					Header: http.Header{
						"Date": []string{
							now.Add(-5 * time.Minute).Format(http.TimeFormat),
							now.Add(-10 * time.Minute).Format(http.TimeFormat),
						},
					},
				}
			},
			want: time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()

				resp := tt.resp(now)
				resp.Time = now

				synctest.Sleep(time.Minute)

				if got := resp.Age(); got != tt.want {
					t.Errorf("ResponseData.Age() = %s, want %s", got, tt.want)
				}
			})
		})
	}
}

func TestResponseData_CacheGroups(t *testing.T) {
	tests := []struct {
		name    string
		resp    httpcache.ResponseData
		want    []string
		wantErr bool
	}{
		{
			name: "no Cache-Groups header",
			resp: httpcache.ResponseData{},
		},
		{
			name: "single Cache-Groups header",
			resp: httpcache.ResponseData{Header: http.Header{
				"Cache-Groups": []string{`"group-a", "group-b", "group-c", "group-b"`},
			}},
			want: []string{"group-a", "group-b", "group-c", "group-b"},
		},
		{
			name: "multiple Cache-Groups headers",
			resp: httpcache.ResponseData{
				Header: http.Header{
					"Cache-Groups": []string{
						`"group-a", "group-b", "group-c", "group-b"`,
						`"group-c"`,
						`"group-a", "group-d"`,
					},
				},
			},
			want: []string{"group-a", "group-b", "group-c", "group-b", "group-c", "group-a", "group-d"},
		},
		{
			name: "parameters are ignored",
			resp: httpcache.ResponseData{Header: http.Header{
				"Cache-Groups": []string{`"group-a";parameter=value, "group-b"`},
			}},
			want: []string{"group-a", "group-b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				got, gotErr := tt.resp.CacheGroups()
				if !slices.Equal(got, tt.want) {
					t.Errorf("ResponseData.CacheGroups() = %v, want %v", got, tt.want)
				}

				if gotErr != nil && !tt.wantErr {
					t.Errorf("ResponseData.CacheGroups() err = %v, want nil", gotErr)
				} else if gotErr == nil && tt.wantErr {
					t.Error("ResponseData.CacheGroups() err = nil, want non-nil")
				}
			})
		})
	}
}

func TestResponseData_Clone(t *testing.T) {
	orig := &httpcache.ResponseData{
		Time:       time.Now(),
		Status:     "Some status",
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Header-1": []string{"Value-1"},
			"Header-2": []string{"Value-2", "Value-3"},
		},
		Body: []byte("request body"),
		Trailer: http.Header{
			"Trailer-1": []string{"Value-4"},
			"Trailer-2": []string{"Value-5", "Value-6"},
		},
		ParsedDirectives: httpcache.ResponseDirectives{
			NoCache: true,
			NoStore: true,
			Extensions: httpcache.ExtensionDirectives{
				{Name: "Extension-1"},
				{Name: "Extension-2", Value: httpcache.Opt[string]{Value: "Value", Valid: true}},
			},
		},
	}

	cloned := orig.Clone()

	if diff := cmp.Diff(orig, cloned); diff != "" {
		t.Errorf("ResponseData.Clone() mismatch (-want +got):\n%s", diff)
	}

	if mapsIdentical(orig.Header, cloned.Header) {
		t.Errorf("ResponseData.Clone() returned same Header map")
	}

	for k := range orig.Header {
		if got, want := orig.Header[k], cloned.Header[k]; slicesIdentical(got, want) {
			t.Errorf("ResponseData.Clone() returned same Header %q slice", k)
		}
	}

	if mapsIdentical(orig.Trailer, cloned.Trailer) {
		t.Errorf("ResponseData.Clone() returned same Trailer map")
	}

	for k := range orig.Trailer {
		if got, want := orig.Trailer[k], cloned.Trailer[k]; slicesIdentical(got, want) {
			t.Errorf("ResponseData.Clone() returned same Trailer %q slice", k)
		}
	}

	if slicesIdentical(orig.ParsedDirectives.Extensions, cloned.ParsedDirectives.Extensions) {
		t.Errorf("ResponseData.Clone() returned same ExtensionDirectives slice")
	}
}

func TestResponseData_Date(t *testing.T) {
	awesomeDate := time.Date(2006, time.January, 2, 15, 4, 5, 0, time.UTC)

	tests := []struct {
		name   string
		resp   httpcache.ResponseData
		want   time.Time
		wantOk bool
	}{
		{
			name: "no Date header",
			resp: httpcache.ResponseData{},
		},
		{
			name: "single Date header",
			resp: httpcache.ResponseData{
				Header: http.Header{
					"Date": []string{awesomeDate.Format(http.TimeFormat)},
				},
			},
			want:   awesomeDate,
			wantOk: true,
		},
		{
			name: "invalid Date header",
			resp: httpcache.ResponseData{
				Header: http.Header{
					"Date": []string{"invalid"},
				},
			},
		},
		{
			name: "multiple Date headers",
			resp: httpcache.ResponseData{
				Header: http.Header{
					"Date": []string{
						awesomeDate.Format(http.TimeFormat),
						awesomeDate.Add(time.Hour).Format(http.TimeFormat),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOk := tt.resp.Date()
			if got != tt.want {
				t.Errorf("ResponseData.Date() = %s, want %s", got, tt.want)
			}
			if gotOk != tt.wantOk {
				t.Errorf("ResponseData.Date() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}
