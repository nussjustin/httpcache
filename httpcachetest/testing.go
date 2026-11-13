package httpcachetest

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

// TestStore tests that an implementation of [github.com/nussjustin/httpcache.Store].
//
// The function runs multiple test cases using [testing.T.Run].
//
// Each test case calls f once, which must return an empty store.
func TestStore(t *testing.T, f func() httpcache.Store) {
	tests := []struct {
		name         string
		storedReq    *http.Request
		storedResp   *http.Response
		lookupReq    *http.Request
		expectedResp *http.Response
		wantStoreErr bool
	}{
		{
			name:       "found",
			storedReq:  newReq(withReqUrl("https://example.com/path?a=1")),
			storedResp: newResp(),
			lookupReq:  newReq(withReqUrl("https://example.com/path?a=1")),
			expectedResp: newResp(
				withRespHeader("Age", "60")),
		},
		{
			name:       "found for HEAD request",
			storedReq:  newReq(),
			storedResp: newResp(),
			lookupReq:  newReq(withReqMethod("HEAD")),
			expectedResp: newResp(
				withRespHeader("Age", "60")),
		},
		{
			name:       "not found for POST request",
			storedReq:  newReq(),
			storedResp: newResp(),
			lookupReq:  newReq(withReqMethod("POST")),
		},
		{
			name:       "not found for URL with different scheme",
			storedReq:  newReq(withReqUrl("https://example.com/")),
			storedResp: newResp(),
			lookupReq:  newReq(withReqUrl("http://example.org/")),
		},
		{
			name:       "not found for URL with different host",
			storedReq:  newReq(withReqUrl("https://example.com/")),
			storedResp: newResp(),
			lookupReq:  newReq(withReqUrl("https://example.org/")),
		},
		{
			name:       "not found for URL with different path",
			storedReq:  newReq(withReqUrl("https://example.com/a")),
			storedResp: newResp(),
			lookupReq:  newReq(withReqUrl("https://example.com/b")),
		},
		{
			name:       "not found for URL with trailing slash in second request",
			storedReq:  newReq(withReqUrl("https://example.com/path")),
			storedResp: newResp(),
			lookupReq:  newReq(withReqUrl("https://example.com/path/")),
		},
		{
			name:       "not found for URL without trailing in second request",
			storedReq:  newReq(withReqUrl("https://example.com/path/")),
			storedResp: newResp(),
			lookupReq:  newReq(withReqUrl("https://example.com/path")),
		},
		{
			name:       "not found for URL with different parameter values",
			storedReq:  newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(),
			lookupReq:  newReq(withReqUrl("https://example.com/path?a=1&b=3")),
		},
		{
			name:      "found for URL with different parameter values and No-Vary-Search params ignoring the parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `params=("b")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=3")),
			expectedResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("No-Vary-Search", `params=("b")`)),
		},
		{
			name:      "not found for URL with different parameter values and No-Vary-Search params ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `params=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=3")),
		},
		{
			name:      "found for URL with different parameter values and No-Vary-Search except ignoring the parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `except=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=3")),
			expectedResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("No-Vary-Search", `except=("a")`)),
		},
		{
			name:      "not found for URL with different parameter values and No-Vary-Search except ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `except=("b")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=3")),
		},
		{
			name:       "not found for URL with extra parameters",
			storedReq:  newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(),
			lookupReq:  newReq(withReqUrl("https://example.com/path?a=1&b=2&c=3")),
		},
		{
			name:      "found for URL with extra parameter and No-Vary-Search params ignoring that parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `params=("c")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=2&c=3")),
			expectedResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("No-Vary-Search", `params=("c")`)),
		},
		{
			name:      "not found for URL with extra parameter and No-Vary-Search params ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `params=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=2&c=3")),
		},
		{
			name:      "found for URL with extra parameter and No-Vary-Search except ignoring that parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `except=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=2&c=3")),
			expectedResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("No-Vary-Search", `except=("a")`)),
		},
		{
			name:      "not found for URL with extra parameter and No-Vary-Search except ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `except=("c")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=2&c=3")),
		},
		{
			name:       "not found for URL with missing parameters",
			storedReq:  newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(),
			lookupReq:  newReq(withReqUrl("https://example.com/path?a=1")),
		},
		{
			name:      "found for URL with missing parameter and No-Vary-Search params ignoring that parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `params=("b")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1")),
			expectedResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("No-Vary-Search", `params=("b")`)),
		},
		{
			name:      "not found for URL with missing parameter and No-Vary-Search params ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `params=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1")),
		},
		{
			name:      "found for URL with missing parameter and No-Vary-Search except ignoring that parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `except=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1")),
			expectedResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("No-Vary-Search", `except=("a")`)),
		},
		{
			name:      "not found for URL with missing parameter and No-Vary-Search except ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `except=("b")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1")),
		},
		{
			name:       "not found for URL with different parameter order",
			storedReq:  newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(),
			lookupReq:  newReq(withReqUrl("https://example.com/path?b=2&a=1")),
		},
		{
			name:      "found for URL with different parameter order and No-Vary-Search with key-order",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", "key-order")),
			lookupReq: newReq(withReqUrl("https://example.com/path?b=2&a=1")),
			expectedResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("No-Vary-Search", "key-order")),
		},
		{
			name:      "error returned when storing response with invalid No-Vary-Search",
			storedReq: newReq(withReqUrl("https://example.com/?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("No-Vary-Search", `key-order=invalid`)),
			wantStoreErr: true,
		},
		{
			name:      "found expired",
			storedReq: newReq(),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=30")),
			lookupReq: newReq(),
			expectedResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("Cache-Control", "public, max-age=30")),
		},

		{
			name: "found for response with Vary header and matching header values",
			storedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
			storedResp: newResp(
				withRespHeader("Vary", "Header-1, Header-2")),
			lookupReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Changed value")),
			expectedResp: newResp(
				withRespHeader("Age", "60"),
				withRespHeader("Vary", "Header-1, Header-2")),
		},
		{
			name: "not found for response with Vary header and non-matching header values",
			storedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Changed value"),
				withReqHeader("Header-3", "Value-3")),
			storedResp: newResp(
				withRespHeader("Vary", "Header-1, Header-2")),
			lookupReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
		},
		{
			name: "not found for response with Vary header cnotaining wildcard",
			storedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
			storedResp: newResp(
				withRespHeader("Vary", "*")),
			lookupReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
		},

		{
			name:      "error returned for response whose body can not be read",
			storedReq: newReq(),
			storedResp: newResp(
				withRespBody(errReader{}),
				withRespHeader("Cache-Control", "public, max-age=30")),
			wantStoreErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := f()

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

				gotResp, gotErr := s.Get(t.Context(), tt.lookupReq)

				if gotResp != nil && gotResp.Body != nil {
					_ = gotResp.Body.Close()
				}

				if gotErr != nil {
					t.Fatalf("Get() error = %v, want nil", gotErr)
				}

				if (tt.expectedResp == nil) != (gotResp == nil) {
					t.Fatalf("Get() got resp = %#v, want %#v", gotResp, tt.expectedResp)
				}

				if tt.expectedResp != nil {
					if got, want := gotResp.StatusCode, tt.expectedResp.StatusCode; got != want {
						t.Errorf("Get() Response.StatusCode = %d, want %d", got, want)
					}

					if got, want := gotResp.Header, tt.expectedResp.Header; want != nil && !headerEqual(got, want) {
						t.Errorf("Get() Response.Header = %#v, want %#v", got, want)
					}
				}
			})
		})
	}
}
