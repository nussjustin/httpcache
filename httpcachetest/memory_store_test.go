package httpcachetest_test

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nussjustin/httpcache/httpcachetest"
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

func TestMemoryStore(t *testing.T) {
	tests := []struct {
		name         string
		storedReq    *http.Request
		storedResp   *http.Response
		fetchedReq   *http.Request
		wantResp     *http.Response
		wantStoreErr bool
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
			name:      "no-vary-search",
			storedReq: newReq(withReqUrl("https://example.com/?a=1&b=2&c=3")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=30"),
				withRespHeader("No-Vary-Search", "key-order, params=(\"b\")"),
				withRespHeader("Transaction-Id", "0")),
			fetchedReq: newReq(withReqUrl("https://example.com/?c=3&a=1")),
			wantResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("Cache-Control", "public, max-age=30"),
				withRespHeader("No-Vary-Search", "key-order, params=(\"b\")"),
				withRespHeader("Transaction-Id", "0")),
		},
		{
			name:      "no-vary-search miss",
			storedReq: newReq(withReqUrl("https://example.com/?a=1&b=2&c=3")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=30"),
				withRespHeader("No-Vary-Search", "key-order, params=(\"b\")"),
				withRespHeader("Transaction-Id", "0")),
			fetchedReq: newReq(withReqUrl("https://example.com/?c=3&a=2")),
		},
		{
			name:      "invalid no-vary-search",
			storedReq: newReq(withReqUrl("https://example.com/?a=1&b=2&c=3")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=30"),
				withRespHeader("No-Vary-Search", "key-order=invalid"),
				withRespHeader("Transaction-Id", "0")),
			wantStoreErr: true,
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
		{
			name:      "body read error",
			storedReq: newReq(),
			storedResp: newResp(
				withRespBody(errReader{}),
				withRespHeader("Cache-Control", "public, max-age=30"),
				withRespHeader("Transaction-Id", "0")),
			wantStoreErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var s httpcachetest.MemoryStore

				if err := s.Set(t.Context(), tt.storedReq, time.Now(), tt.storedResp, time.Now()); err != nil {
					if tt.wantStoreErr {
						return
					}
					t.Fatalf("Set() error = %v, want nil", err)
				}

				if tt.wantStoreErr {
					t.Fatal("Set() error = nil, want non-nil")
				}

				time.Sleep(time.Minute)

				gotResp, gotErr := s.Get(t.Context(), tt.fetchedReq)

				if gotResp != nil && gotResp.Body != nil {
					_ = gotResp.Body.Close()
				}

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
