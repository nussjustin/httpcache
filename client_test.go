package httpcache_test

import (
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

	"github.com/nussjustin/httpcache"
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

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackingStore struct {
	httpcache.Store
	failOnGet   bool
	failOnStore bool
	stored      int
}

var errStoreFail = errors.New("fail on store")

func (t *trackingStore) Get(ctx context.Context, req *http.Request) (resp *http.Response, err error) {
	if t.failOnGet {
		return nil, errStoreFail
	}
	return t.Store.Get(ctx, req)
}

func (t *trackingStore) Set(
	ctx context.Context,
	req *http.Request, reqTime time.Time,
	resp *http.Response, respTime time.Time,
) error {
	if t.failOnStore {
		return errStoreFail
	}
	t.stored++
	return t.Store.Set(ctx, req, reqTime, resp, respTime)
}

type reqOpt func(*http.Request)

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

func TestClient_Do(t *testing.T) {
	type transaction struct {
		req         *http.Request
		resp        *http.Response
		respErr     error
		failOnGet   bool
		failOnStore bool
		wantStored  int
		wantReq     *http.Request
		wantResp    *http.Response
		wantRespErr bool
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
					req:        newReq(),
					resp:       newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req:     newReq(),
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
					req:        newReq(),
					resp:       newResp(withRespHeader("Cache-Control", "public, max-age=60")),
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req:        newReq(),
					resp:       newResp(withRespHeader("Cache-Control", "public, max-age=60")),
					wantStored: 1,
					wantReq:    newReq(),
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
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"other tag"`)),
					wantStored: 1,
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
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Last-Modified", `Mon, 03 Jan 2006 15:04:05 GMT`)),
					wantStored: 1,
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
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"other etag"`),
						withRespHeader("Last-Modified", `Mon, 02 Jan 2006 15:04:05 GMT`)),
					wantStored: 1,
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
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"my etag"`),
						withRespHeader("Last-Modified", `Mon, 03 Jan 2006 15:04:05 GMT`)),
					wantStored: 1,
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
					resp: newResp(
						withRespHeader("Cache-Control", "public, max-age=60"),
						withRespHeader("Etag", `"other etag"`),
						withRespHeader("Last-Modified", `Mon, 03 Jan 2006 15:04:05 GMT`)),
					wantStored: 1,
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
					wantResp: newResp(
						withRespStatus(http.StatusGatewayTimeout)),
				},
				{
					req:        newReq(),
					resp:       newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "1")),
				},
				{
					req:     newReq(withReqHeader("Cache-Control", "only-if-cached")),
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
					req:        newReq(),
					resp:       newResp(withRespHeader("Cache-Control", "public, max-age=120, no-store")),
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
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Connection", "close"),
						withRespHeader("Proxy-Authenticate", "Basis"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req:     newReq(),
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
					req:        newReq(withReqMethod("POST")),
					resp:       newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantStored: 0,
					wantReq:    newReq(withReqMethod("POST")),
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
					req:        newReq(withReqMethod("POST")),
					resp:       newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantStored: 1,
					wantReq:    newReq(withReqMethod("POST")),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "expect header set",
			txs: []transaction{
				{
					req:        newReq(withReqHeader("Expect", "")),
					resp:       newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantStored: 0,
					wantReq:    newReq(withReqHeader("Expect", "")),
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
					req:        newReq(),
					resp:       newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					wantStored: 1,
					wantReq:    newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
				{
					req:        newReq(),
					resp:       newResp(withRespHeader("Cache-Control", "public, max-age=120")),
					failOnGet:  true,
					wantStored: 1,
					wantReq:    newReq(),
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
					wantReq:     newReq(),
					wantResp: newResp(
						withRespHeader("Cache-Control", "public, max-age=120"),
						withRespHeader("Transaction-Id", "0")),
				},
			},
		},
		{
			name: "error when cloning response",
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				memStore := httpcache.NewMemoryStore()

				for i, tx := range tt.txs {
					if tx.resp != nil {
						tx.resp.Header.Set("Transaction-Id", strconv.Itoa(i))
					}

					store := &trackingStore{
						Store:       memStore,
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

								if tx.respErr != nil {
									return nil, tx.respErr
								}

								tx.resp.Request = req

								return tx.resp, nil
							}),
						},
					}).Do(tx.req)

					if (err != nil) != tx.wantRespErr {
						t.Errorf("Tx %d: Do() error = %v, wantErr = %v", i, err, tx.wantRespErr)
						return
					}

					if !tx.wantRespErr {
						if got, want := resp.StatusCode, tx.wantResp.StatusCode; got != want {
							t.Errorf("Tx %d: Do() Response.StatusCode = %d, want %d", i, got, want)
						}

						if got, want := resp.Header, tx.wantResp.Header; want != nil && !headerEqual(got, want) {
							t.Errorf("Tx %d: Do() Response.Header = %#v, want %#v", i, got, want)
						}

						if got, want := store.stored, tx.wantStored; got != want {
							t.Errorf("Tx %d: Do() got Store.Set calls = %v, want %v", i, got, want)
						}
					}

					time.Sleep(time.Minute)
				}
			})
		})
	}
}

func TestMemoryStore(t *testing.T) {
	tests := []struct {
		name       string
		storedReq  *http.Request
		storedResp *http.Response
		fetchedReq *http.Request
		wantResp   *http.Response
	}{
		{
			name:      "match",
			storedReq: newReq(),
			storedResp: newResp(
				withRespHeader("Transaction-Id", "0")),
			fetchedReq: newReq(),
			wantResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("Transaction-Id", "0")),
		},
		{
			name:      "mismatched method",
			storedReq: newReq(),
			storedResp: newResp(
				withRespHeader("Transaction-Id", "0")),
			fetchedReq: newReq(withReqMethod("HEAD")),
		},
		{
			name:      "mismatched url",
			storedReq: newReq(),
			storedResp: newResp(
				withRespHeader("Transaction-Id", "0")),
			fetchedReq: newReq(withReqUrl("https://example.com/")),
		},
		{
			name: "vary match",
			storedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
			storedResp: newResp(
				withRespHeader("Transaction-Id", "0"),
				withRespHeader("Vary", "Header-1, Header-2")),
			fetchedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Changed value")),
			wantResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("Transaction-Id", "0"),
				withRespHeader("Vary", "Header-1, Header-2")),
		},
		{
			name: "vary mismatch",
			storedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Changed value"),
				withReqHeader("Header-3", "Value-3")),
			storedResp: newResp(
				withRespHeader("Transaction-Id", "0"),
				withRespHeader("Vary", "Header-1, Header-2")),
			fetchedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
		},
		{
			name: "vary wildcard",
			storedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
			storedResp: newResp(
				withRespHeader("Transaction-Id", "0"),
				withRespHeader("Vary", "*")),
			fetchedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
		},
		{
			name:      "expired",
			storedReq: newReq(),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=30"),
				withRespHeader("Transaction-Id", "0")),
			fetchedReq: newReq(),
			wantResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("Cache-Control", "public, max-age=30"),
				withRespHeader("Transaction-Id", "0")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := httpcache.NewMemoryStore()

				if err := s.Set(t.Context(), tt.storedReq, time.Now(), tt.storedResp, time.Now()); err != nil {
					t.Fatalf("Set() error = %v, want nil", err)
				}

				time.Sleep(time.Minute)

				gotResp, gotErr := s.Get(t.Context(), tt.fetchedReq)

				if gotErr != nil {
					t.Fatalf("Get() error = %v, want nil", gotErr)
				}

				if (tt.wantResp == nil) != (gotResp == nil) {
					t.Fatalf("Get() got resp = %#v, want %#v", gotResp, tt.wantResp)
				}

				if tt.wantResp != nil {
					if got, want := gotResp.StatusCode, tt.wantResp.StatusCode; got != want {
						t.Errorf("Get() Response.StatusCode = %d, want %d", got, want)
					}

					if got, want := gotResp.Header, tt.wantResp.Header; want != nil && !headerEqual(got, want) {
						t.Errorf("Get() Response.Header = %#v, want %#v", got, want)
					}
				}
			})
		})
	}
}
