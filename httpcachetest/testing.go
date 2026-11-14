package httpcachetest

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
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

type reqOpt func(*httpcache.RequestData)

func withReqBody(b string) reqOpt {
	return func(req *httpcache.RequestData) {
		req.Body = []byte(b)
	}
}

func withReqHeader(name, value string) reqOpt {
	return func(req *httpcache.RequestData) {
		req.Header.Add(name, value)
	}
}

func withReqMethod(method string) reqOpt {
	return func(req *httpcache.RequestData) {
		req.Method = method
	}
}

func withReqUrl(urlStr string) reqOpt {
	return func(req *httpcache.RequestData) {
		parsedURL, err := url.Parse(urlStr)
		if err != nil {
			panic(err)
		}
		req.URL = *parsedURL
	}
}

func newReq(opts ...reqOpt) *httpcache.RequestData {
	req := &httpcache.RequestData{Header: make(http.Header)}

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

type respOpt func(*httpcache.ResponseData)

func withRespBody(b string) respOpt {
	return func(resp *httpcache.ResponseData) {
		resp.Body = []byte(b)
	}
}

func withRespHeader(name, value string) respOpt {
	return func(resp *httpcache.ResponseData) {
		resp.Header.Add(name, value)
	}
}

func withRespStatus(code int) respOpt {
	return func(resp *httpcache.ResponseData) {
		resp.Status = http.StatusText(code)
		resp.StatusCode = code
	}
}

// bodyclose:handled
func newResp(opts ...respOpt) *httpcache.ResponseData {
	resp := &httpcache.ResponseData{Header: make(http.Header), Body: []byte{}}

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

// TestStoreOpts can be used to configure what [TestStore] expects for certain test cases that have no strictly defined
// behavior.
type TestStoreOpts struct {
	// ReturnsNilForExpired must be set to true if Get returns no response when the response is expired.
	//
	// Expiry must be calculated based as defined in RFC 9111, taking into account the response Cache-Control and
	// Expires headers.
	ReturnsNilForExpiredResponses bool

	// ReturnsNilForExpiredResponsesAfter is the duration after a response is expired after which a store returns no
	// response.
	//
	// Requires ReturnsNilForExpiredResponses to be true.
	//
	// Must be in full seconds, and in the range [1, 60).
	ReturnsNilForExpiredResponsesAfter time.Duration

	// ValidatesNoVarySearch must be set to true when the Set method on a Store returns an error if the
	// No-Vary-Search header is invalid.
	ValidatesNoVarySearch bool
}

// TestStore tests that an implementation of [github.com/nussjustin/httpcache.Store].
//
// The function runs multiple test cases using [testing.T.Run].
//
// Each test case calls f once, which must return an empty store.
func TestStore(t *testing.T, cfg TestStoreOpts, f func() httpcache.Store) {
	type testCase struct {
		name          string
		storedReq     *httpcache.RequestData
		storedResp    *httpcache.ResponseData
		lookupReq     *httpcache.RequestData
		expectedResp  *httpcache.ResponseData
		wantStoreErr  bool
		wantLookupErr bool
	}

	tests := []testCase{
		{
			name:      "found",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespBody(`hello world`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1")),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespBody(`hello world`)),
		},
		{
			name:      "found for HEAD request",
			storedReq: newReq(),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqMethod("HEAD")),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
		},
		{
			name:      "not found for POST request",
			storedReq: newReq(),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqMethod("POST")),
		},
		{
			name: "found with body",
			storedReq: newReq(
				withReqMethod("QUERY"),
				withReqHeader("Content-Type", "application/json"),
				withReqBody(`{"query": "test"}`)),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(
				withReqMethod("QUERY"),
				withReqHeader("Content-Type", "application/json"),
				withReqBody(`{"query": "test"}`)),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
		},
		{
			name: "not found with body mismatch",
			storedReq: newReq(
				withReqMethod("QUERY"),
				withReqHeader("Content-Type", "application/json"),
				withReqBody(`{"query": "test"}`)),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(
				withReqMethod("QUERY"),
				withReqHeader("Content-Type", "application/json"),
				withReqBody(`{"query": "test1"}`)),
		},
		{
			name:      "not found for URL with different scheme",
			storedReq: newReq(withReqUrl("https://example.com/")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqUrl("http://example.org/")),
		},
		{
			name:      "not found for URL with different host",
			storedReq: newReq(withReqUrl("https://example.com/")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqUrl("https://example.org/")),
		},
		{
			name:      "not found for URL with different path",
			storedReq: newReq(withReqUrl("https://example.com/a")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqUrl("https://example.com/b")),
		},
		{
			name:      "not found for URL with trailing slash in second request",
			storedReq: newReq(withReqUrl("https://example.com/path")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqUrl("https://example.com/path/")),
		},
		{
			name:      "not found for URL without trailing in second request",
			storedReq: newReq(withReqUrl("https://example.com/path/")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqUrl("https://example.com/path")),
		},
		{
			name:      "not found for URL with different parameter values",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=3")),
		},
		{
			name:      "found for URL with different parameter values and No-Vary-Search params ignoring the parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `params=("b")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=3")),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `params=("b")`)),
		},
		{
			name:      "not found for URL with different parameter values and No-Vary-Search params ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `params=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=3")),
		},
		{
			name:      "found for URL with different parameter values and No-Vary-Search except ignoring the parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `except=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=3")),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `except=("a")`)),
		},
		{
			name:      "not found for URL with different parameter values and No-Vary-Search except ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `except=("b")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=3")),
		},
		{
			name:      "not found for URL with extra parameters",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=2&c=3")),
		},
		{
			name:      "found for URL with extra parameter and No-Vary-Search params ignoring that parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `params=("c")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=2&c=3")),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `params=("c")`)),
		},
		{
			name:      "not found for URL with extra parameter and No-Vary-Search params ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `params=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=2&c=3")),
		},
		{
			name:      "found for URL with extra parameter and No-Vary-Search except ignoring that parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `except=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=2&c=3")),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `except=("a")`)),
		},
		{
			name:      "not found for URL with extra parameter and No-Vary-Search except ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `except=("c")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1&b=2&c=3")),
		},
		{
			name:      "not found for URL with missing parameters",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1")),
		},
		{
			name:      "found for URL with missing parameter and No-Vary-Search params ignoring that parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `params=("b")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1")),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `params=("b")`)),
		},
		{
			name:      "not found for URL with missing parameter and No-Vary-Search params ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `params=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1")),
		},
		{
			name:      "found for URL with missing parameter and No-Vary-Search except ignoring that parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `except=("a")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1")),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `except=("a")`)),
		},
		{
			name:      "not found for URL with missing parameter and No-Vary-Search except ignoring a different parameter",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `except=("b")`)),
			lookupReq: newReq(withReqUrl("https://example.com/path?a=1")),
		},
		{
			name:      "not found for URL with different parameter order",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120")),
			lookupReq: newReq(withReqUrl("https://example.com/path?b=2&a=1")),
		},
		{
			name:      "found for URL with different parameter order and No-Vary-Search with key-order",
			storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", "key-order")),
			lookupReq: newReq(withReqUrl("https://example.com/path?b=2&a=1")),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", "key-order")),
		},

		{
			name: "found for response with Vary header and matching header values",
			storedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("Vary", "Header-1, Header-2")),
			lookupReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Changed value")),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("Vary", "Header-1, Header-2")),
		},
		{
			name: "not found for response with Vary header and non-matching header values",
			storedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Changed value"),
				withReqHeader("Header-3", "Value-3")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("Vary", "Header-1, Header-2")),
			lookupReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
		},
		{
			name: "not found for response with Vary header containing wildcard",
			storedReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("Vary", "*")),
			lookupReq: newReq(
				withReqHeader("Header-1", "Value-1"),
				withReqHeader("Header-2", "Value-2"),
				withReqHeader("Header-3", "Value-3")),
		},
	}

	if cfg.ReturnsNilForExpiredResponses {
		nilAfter := int(((60 * time.Second) - cfg.ReturnsNilForExpiredResponsesAfter).Seconds())

		tests = append(tests, []testCase{
			{
				name:      "found response with 1 second left before expiry",
				storedReq: newReq(),
				storedResp: newResp(
					withRespHeader("Cache-Control", "public, max-age=61")),
				lookupReq: newReq(),
				expectedResp: newResp(
					withRespHeader("Cache-Control", "public, max-age=61")),
			},
			{
				name:      "not-found response when store TTL is reached",
				storedReq: newReq(),
				storedResp: newResp(
					withRespHeader("Cache-Control", "public, max-age="+strconv.Itoa(nilAfter))),
				lookupReq: newReq(),
			},
			{
				name:      "not-found response a second after store TTL was reached",
				storedReq: newReq(),
				storedResp: newResp(
					withRespHeader("Cache-Control", "public, max-age="+strconv.Itoa(nilAfter-1))),
				lookupReq: newReq(),
			},
		}...)
	} else {
		tests = append(tests, testCase{
			name:      "found expired response",
			storedReq: newReq(),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=5")),
			lookupReq: newReq(),
			expectedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=5")),
		})
	}

	if cfg.ValidatesNoVarySearch {
		tests = append(tests, testCase{
			name:      "error returned when storing response with invalid No-Vary-Search",
			storedReq: newReq(),
			storedResp: newResp(
				withRespHeader("Cache-Control", "public, max-age=120"),
				withRespHeader("No-Vary-Search", `key-order=invalid`)),
			wantStoreErr: true,
		})
	} else {
		tests = append(tests, []testCase{
			{
				name:      "found for response with invalid No-Vary-Search",
				storedReq: newReq(),
				storedResp: newResp(
					withRespHeader("Cache-Control", "public, max-age=120"),
					withRespHeader("No-Vary-Search", `key-order=invalid`)),
				lookupReq: newReq(),
				expectedResp: newResp(
					withRespHeader("Cache-Control", "public, max-age=120"),
					withRespHeader("No-Vary-Search", `key-order=invalid`)),
			},
			{
				name:      "not found for response with invalid No-Vary-Search and different parameter order",
				storedReq: newReq(withReqUrl("https://example.com/path?a=1&b=2")),
				storedResp: newResp(
					withRespHeader("Cache-Control", "public, max-age=120"),
					withRespHeader("No-Vary-Search", `key-order=invalid`)),
				lookupReq: newReq(withReqUrl("https://example.com/path?b=2&b=1")),
			},
		}...)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := f()

				storedReq := *tt.storedReq
				storedReq.Time = time.Now()

				storedResp := *tt.storedResp
				storedResp.Time = time.Now()

				if _, err := s.Set(t.Context(), storedReq, storedResp); err != nil {
					if tt.wantStoreErr {
						return
					}
					t.Fatalf("Set() error = %v, want nil", err)
				}

				if tt.wantStoreErr {
					t.Fatal("Set() error = nil, want non-nil")
				}

				time.Sleep(time.Minute)
				synctest.Wait()

				gotResp, _, gotErr := s.Get(t.Context(), *tt.lookupReq)

				switch {
				case tt.wantLookupErr && gotErr == nil:
					t.Fatal("Get() error = nil, want error")
				case !tt.wantLookupErr && gotErr != nil:
					t.Fatalf("Get() error = %v, want nil", gotErr)
				}

				switch {
				case tt.expectedResp == nil && gotResp != nil:
					t.Fatalf("Get() resp = %s, want nil", formatRespForError(t, gotResp))
				case tt.expectedResp != nil && gotResp == nil:
					t.Fatalf("Get() resp = nil, want %s", formatRespForError(t, tt.expectedResp))
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

func formatRespForError(tb testing.TB, resp *httpcache.ResponseData) string {
	type respForError struct {
		Status     string      `json:"Status"`
		StatusCode int         `json:"StatusCode"`
		Header     http.Header `json:"Header"`
		Body       string      `json:"Body,omitempty"`
		Trailer    http.Header `json:"Trailer,omitempty"`
	}

	b, err := json.Marshal(respForError{
		Status:     resp.Status,
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       string(resp.Body),
		Trailer:    resp.Trailer,
	})
	if err != nil {
		tb.Fatalf("failed to marshal response data to JSON: %s", err)
	}

	return string(b)
}
