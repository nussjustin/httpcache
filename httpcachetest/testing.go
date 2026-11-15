package httpcachetest

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/nussjustin/httpcache"
)

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

func newReq(opts ...reqOpt) httpcache.RequestData {
	req := httpcache.RequestData{Header: make(http.Header)}

	opts = append(
		[]reqOpt{
			withReqMethod("GET"),
			withReqUrl("http://example.com/"),
		},
		opts...,
	)

	for _, opt := range opts {
		opt(&req)
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

func newResp(opts ...respOpt) httpcache.ResponseData {
	resp := httpcache.ResponseData{Header: make(http.Header), Body: []byte{}}

	opts = append(
		[]respOpt{
			withRespStatus(http.StatusOK),
		},
		opts...,
	)

	for _, opt := range opts {
		opt(&resp)
	}

	return resp
}

// TestStoreOpts can be used to configure what [TestStore] expects for certain test cases that have no strictly defined
// behavior.
type TestStoreOpts struct {
	// DefaultPortMatchesExplicitPort must be set to true for caches that can return a stored response for an URL with
	// default port (e.g. 443 for HTTPS) for one with an explicit port and vice versa.
	DefaultPortMatchesExplicitPort bool

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

	// SupportsCacheGroups must be true if the Store supports invalidation via cache groups.
	SupportsCacheGroups bool

	// ValidatesNoVarySearch must be set to true when the Set method on a Store returns an error if the
	// No-Vary-Search header is invalid.
	ValidatesNoVarySearch bool
}

type storeOperation interface {
	do(tb testing.TB, s httpcache.Store)
}

type doGet struct {
	args       doGetArgs
	want       doGetWant
	timeOffset time.Duration
}

var _ storeOperation = doGet{}

type doGetArgs struct {
	req httpcache.RequestData
}

type doGetWant struct {
	resp *httpcache.ResponseData
	err  error
}

func (d doGet) do(tb testing.TB, s httpcache.Store) {
	if d.args.req.Time.IsZero() {
		d.args.req.Time = time.Now().Add(d.timeOffset)
	}

	if d.want.resp != nil && d.want.resp.Time.IsZero() {
		d.want.resp.Time = time.Now().Add(d.timeOffset)
	}

	gotResp, _, gotErr := s.Get(tb.Context(), d.args.req)
	if diff := cmp.Diff(d.want.resp, gotResp); diff != "" {
		tb.Errorf("Get() mismatch (-want +got):\n%s", diff)
	}

	if !errors.Is(d.want.err, gotErr) {
		tb.Errorf("Get() err mismatch: got %v, want %v", gotErr, d.want.err)
	}
}

type doInvalidate struct {
	args doInvalidateArgs
	want doInvalidateWant
}

var _ storeOperation = doInvalidate{}

type doInvalidateArgs struct {
	url string
}

type doInvalidateWant struct {
	err error
}

func (d doInvalidate) do(tb testing.TB, s httpcache.Store) {
	url_, err := url.Parse(d.args.url)
	if err != nil {
		tb.Fatalf("invalid URL %q: %s", d.args.url, err)
	}

	gotErr := s.Invalidate(tb.Context(), *url_)
	if !errors.Is(d.want.err, gotErr) {
		tb.Errorf("Invalidate() err mismatch: got %v, want %v", gotErr, d.want.err)
	}
}

type doInvalidateGroups struct {
	args doInvalidateGroupsArgs
	want doInvalidateGroupsWant
}

var _ storeOperation = doInvalidateGroups{}

type doInvalidateGroupsArgs struct {
	groups []string
}

type doInvalidateGroupsWant struct {
	err error
}

func (d doInvalidateGroups) do(tb testing.TB, s httpcache.Store) {
	gotErr := s.InvalidateGroups(tb.Context(), d.args.groups...)
	if !errors.Is(d.want.err, gotErr) {
		tb.Errorf("InvalidateGroups() err mismatch: got %v, want %v", gotErr, d.want.err)
	}
}

type doSet struct {
	args doSetArgs
	want doSetWant
}

var _ storeOperation = doSet{}

type doSetArgs struct {
	req  httpcache.RequestData
	resp httpcache.ResponseData
}

type doSetWant struct {
	err bool
}

func (d doSet) do(tb testing.TB, s httpcache.Store) {
	if d.args.req.Time.IsZero() {
		d.args.req.Time = time.Now()
	}

	if d.args.resp.Time.IsZero() {
		d.args.resp.Time = time.Now()
	}

	_, gotErr := s.Set(tb.Context(), d.args.req, d.args.resp)
	if gotErr != nil && !d.want.err {
		tb.Errorf("Set() err = %v, want nil", gotErr)
	} else if gotErr == nil && d.want.err {
		tb.Errorf("Set() err = nil, want non-nil")
	}
}

type doSleep struct {
	time time.Duration
}

var _ storeOperation = doSleep{}

func (d doSleep) do(testing.TB, httpcache.Store) {
	time.Sleep(d.time)
	synctest.Wait()
}

// TestStore tests an implementation of [github.com/nussjustin/httpcache.Store] against the expectation set by the
// interface.
//
// The function runs multiple test cases using [testing.T.Run] and inside a [testing/synctest] bubble.
//
// Each test case calls f once, which must return an empty store.
func TestStore(t *testing.T, cfg TestStoreOpts, f func() httpcache.Store) {
	type testCase struct {
		name string
		ops  []storeOperation
	}

	tests := []testCase{
		{
			name: "get on empty",
			ops: []storeOperation{
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET")),
					},
				},
			},
		},
		{
			name: "set + hit",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set + hit HEAD request",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("HEAD"),
							withReqUrl("https://example.com/path")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set with explicit port + hit with explicit port",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com:1443/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com:1443/path")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set + miss by scheme",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("http://example.com/path")),
					},
				},
			},
		},
		{
			name: "set + miss by host",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.org/path")),
					},
				},
			},
		},
		{
			name: "set + miss by path",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path2")),
					},
				},
			},
		},
		{
			name: "set + miss by path with extra trailing /",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path/")),
					},
				},
			},
		},
		{
			name: "set + miss by path with missing trailing /",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path/")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
					},
				},
			},
		},
		{
			name: "set + miss by query",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?query")),
					},
				},
			},
		},
		{
			name: "set + miss by query with changed parameter",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=2")),
					},
				},
			},
		},
		{
			name: "set + hit with changed query parameter and No-Vary-Search params",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `params=("a")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=2")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `params=("a")`),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set + miss with changed query parameter and No-Vary-Search params",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `params=("b")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=2")),
					},
				},
			},
		},
		{
			name: "set + hit with changed query parameter and No-Vary-Search except",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `except=("b")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=2")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `except=("b")`),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set + miss with changed query parameter and No-Vary-Search except",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `except=("a")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=2")),
					},
				},
			},
		},
		{
			name: "set + miss by query with added parameter",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
					},
				},
			},
		},
		{
			name: "set + hit with added query parameter and No-Vary-search params",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `params=("b")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `params=("b")`),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set + miss with added query parameter and No-Vary-search params",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `params=("a")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
					},
				},
			},
		},
		{
			name: "set + hit with added query parameter and No-Vary-search except",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `except=("a")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `except=("a")`),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set + miss with added query parameter and No-Vary-search except",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `except=("b")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
					},
				},
			},
		},
		{
			name: "set + miss by query with removed parameter",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
					},
				},
			},
		},
		{
			name: "set + hit with removed query parameter and No-Vary-search params",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `params=("b")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `params=("b")`),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set + miss with removed query parameter and No-Vary-search params",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `params=("a")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
					},
				},
			},
		},
		{
			name: "set + hit with removed query parameter and No-Vary-search except",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `except=("a")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `except=("a")`),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set + miss with removed query parameter and No-Vary-search params",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", `except=("b")`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
					},
				},
			},
		},
		{
			name: "set + miss by query with changed key-order",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?b=2&a=1")),
					},
				},
			},
		},
		{
			name: "set + hit with changed query and No-Vary-search key-order",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1&b=2")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", "key-order"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?b=2&a=1")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", "key-order"),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set with body + hit",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("QUERY"),
							withReqUrl("https://example.com/path"),
							withReqBody(`request body`)),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("QUERY"),
							withReqUrl("https://example.com/path"),
							withReqBody(`request body`)),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set with body + miss by body",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("QUERY"),
							withReqUrl("https://example.com/path"),
							withReqBody(`request body`)),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("QUERY"),
							withReqUrl("https://example.com/path"),
							withReqBody(`request body 2`)),
					},
				},
			},
		},
		{
			name: "set with Vary + hit",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path"),
							withReqHeader("Header-1", "Value-1"),
							withReqHeader("Header-2", "Value-2"),
							withReqHeader("Header-3", "Value-3"),
							withReqHeader("Header-4", "Ignored")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Vary", "Header-1, Header-2"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path"),
							withReqHeader("Header-1", "Value-1"),
							withReqHeader("Header-2", "Value-2"),
							withReqHeader("Header-3", "Value-3"),
							withReqHeader("Header-5", "Ignored")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Vary", "Header-1, Header-2"),
							withRespBody(`response body`))),
					},
				},
			},
		},
		{
			name: "set with Vary + miss by changed header",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path"),
							withReqHeader("Header-1", "Value-1"),
							withReqHeader("Header-2", "Value-2"),
							withReqHeader("Header-3", "Value-3")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Vary", "Header-1, Header-2"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path"),
							withReqHeader("Header-1", "Value-1"),
							withReqHeader("Header-2", "Value-2+"),
							withReqHeader("Header-3", "Value-3")),
					},
				},
			},
		},
		{
			name: "set with Vary + miss by changed header with duplicate value",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path"),
							withReqHeader("Header-1", "Value-1"),
							withReqHeader("Header-2", "Value-2"),
							withReqHeader("Header-3", "Value-3")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Vary", "Header-1, Header-2"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path"),
							withReqHeader("Header-1", "Value-1"),
							withReqHeader("Header-2", "Value-2"),
							withReqHeader("Header-2", "Value-2"),
							withReqHeader("Header-3", "Value-3")),
					},
				},
			},
		},
		{
			name: "set with Vary + miss by removed header",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path"),
							withReqHeader("Header-1", "Value-1"),
							withReqHeader("Header-2", "Value-2"),
							withReqHeader("Header-3", "Value-3")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Vary", "Header-1, Header-2"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path"),
							withReqHeader("Header-1", "Value-1"),
							withReqHeader("Header-3", "Value-3")),
					},
				},
			},
		},
		{
			name: "set with Vary containing wildcard + miss",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path"),
							withReqHeader("Header-1", "Value-1"),
							withReqHeader("Header-2", "Value-2")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Vary", "Header-1, *, Header-2"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path"),
							withReqHeader("Header-1", "Value-1"),
							withReqHeader("Header-2", "Value-2")),
					},
				},
			},
		},
		{
			name: "set + invalidate + miss",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path-1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body 1`)),
					},
				},
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path-2")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body 2`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path-1")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body 1`))),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path-2")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body 2`))),
					},
				},
				doInvalidate{
					args: doInvalidateArgs{
						url: "https://example.com/path-1",
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path-1")),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path-2")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body 2`))),
					},
				},
			},
		},
		{
			name: "set + invalidate with explicit port + miss",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body 1`)),
					},
				},
				doInvalidate{
					args: doInvalidateArgs{
						url: "https://example.com:443/path",
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
					},
				},
			},
		},
		{
			name: "set + invalidate with implicit port + miss",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com:443/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body 1`)),
					},
				},
				doInvalidate{
					args: doInvalidateArgs{
						url: "https://example.com/path",
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com:443/path")),
					},
				},
			},
		},
		{
			name: "set + sleep + hit fresh",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doSleep{time: 119 * time.Second},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`))),
					},
					timeOffset: -119 * time.Second,
				},
			},
		},
	}

	if cfg.DefaultPortMatchesExplicitPort {
		tests = append(tests, testCase{
			name: "set with implicit port + hit with explicit port",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com:443/path")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`))),
					},
				},
			},
		})

		tests = append(tests, testCase{
			name: "set with explicit port + hit with implicit port",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com:443/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`))),
					},
				},
			},
		})
	} else {
		tests = append(tests, testCase{
			name: "set with implicit port + miss with explicit port",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com:443/path")),
					},
				},
			},
		})

		tests = append(tests, testCase{
			name: "set with explicit port + miss with implicit port",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com:443/path")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path")),
					},
				},
			},
		})
	}

	if cfg.ReturnsNilForExpiredResponses {
		tests = append(tests, testCase{
			name: "set + sleep + miss expired",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doSleep{time: 120*time.Second + cfg.ReturnsNilForExpiredResponsesAfter},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/")),
					},
				},
			},
		})

		if cfg.ReturnsNilForExpiredResponsesAfter >= 2*time.Second {
			sleep := 120*time.Second + cfg.ReturnsNilForExpiredResponsesAfter - time.Second

			tests = append(tests, testCase{
				name: "set + sleep + hit expired that was not yet removed",
				ops: []storeOperation{
					doSet{
						args: doSetArgs{
							req: newReq(
								withReqMethod("GET"),
								withReqUrl("https://example.com/")),
							resp: newResp(
								withRespHeader("Cache-Control", "max-age=120"),
								withRespBody(`response body`)),
						},
					},
					doSleep{time: sleep},
					doGet{
						args: doGetArgs{
							req: newReq(
								withReqMethod("GET"),
								withReqUrl("https://example.com/")),
						},
						want: doGetWant{
							resp: new(newResp(
								withRespHeader("Cache-Control", "max-age=120"),
								withRespBody(`response body`))),
						},
						timeOffset: -sleep,
					},
				},
			})
		}
	} else {
		sleep := 120 * time.Second

		tests = append(tests, testCase{
			name: "set + sleep + hit expired",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`)),
					},
				},
				doSleep{time: sleep},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespBody(`response body`))),
					},
					timeOffset: -sleep,
				},
			},
		})
	}

	if cfg.SupportsCacheGroups {
		tests = append(tests, testCase{
			name: "set multiple with cache groups + invalidate some groups + hits and misses",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/a")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Cache-Groups", `"group-a"`),
							withRespBody(`response body`)),
					},
				},
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/a-b")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Cache-Groups", `"group-a", "group-b"`),
							withRespBody(`response body`)),
					},
				},
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/b-c")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Cache-Groups", `"group-b", "group-c"`),
							withRespBody(`response body`)),
					},
				},
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/c-d")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Cache-Groups", `"group-c", "group-d"`),
							withRespBody(`response body`)),
					},
				},
				doInvalidateGroups{
					args: doInvalidateGroupsArgs{
						groups: []string{"group-a", "group-b"},
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/a")),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/a-b")),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/b-c")),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/c-d")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Cache-Groups", `"group-c", "group-d"`),
							withRespBody(`response body`))),
					},
				},
			},
		})

		tests = append(tests, testCase{
			name: "set with invalid cache groups + hits",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/a")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Cache-Groups", `"group-a" "group-b"`),
							withRespBody(`response body`)),
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/a")),
					},
					want: doGetWant{
						resp: new(newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("Cache-Groups", `"group-a" "group-b"`),
							withRespBody(`response body`))),
					},
				},
			},
		})
	}

	if cfg.ValidatesNoVarySearch {
		tests = append(tests, testCase{
			name: "set with invalid No-Vary-Search fails + miss",
			ops: []storeOperation{
				doSet{
					args: doSetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
						resp: newResp(
							withRespHeader("Cache-Control", "max-age=120"),
							withRespHeader("No-Vary-Search", "key-order=invalid"),
							withRespBody(`response body`)),
					},
					want: doSetWant{
						err: true,
					},
				},
				doGet{
					args: doGetArgs{
						req: newReq(
							withReqMethod("GET"),
							withReqUrl("https://example.com/path?a=1")),
					},
				},
			},
		})
	} else {
		tests = append(tests, []testCase{
			{
				name: "set with invalid No-Vary-Search succeeds + hit",
				ops: []storeOperation{
					doSet{
						args: doSetArgs{
							req: newReq(
								withReqMethod("GET"),
								withReqUrl("https://example.com/path?a=1")),
							resp: newResp(
								withRespHeader("Cache-Control", "max-age=120"),
								withRespHeader("No-Vary-Search", "key-order=invalid"),
								withRespBody(`response body`)),
						},
					},
					doGet{
						args: doGetArgs{
							req: newReq(
								withReqMethod("GET"),
								withReqUrl("https://example.com/path?a=1")),
						},
						want: doGetWant{
							resp: new(newResp(
								withRespHeader("Cache-Control", "max-age=120"),
								withRespHeader("No-Vary-Search", "key-order=invalid"),
								withRespBody(`response body`))),
						},
					},
				},
			},
			{
				name: "set with invalid No-Vary-Search succeeds + miss with different query",
				ops: []storeOperation{
					doSet{
						args: doSetArgs{
							req: newReq(
								withReqMethod("GET"),
								withReqUrl("https://example.com/path?a=1&b=2")),
							resp: newResp(
								withRespHeader("Cache-Control", "max-age=120"),
								withRespHeader("No-Vary-Search", "key-order=invalid"),
								withRespBody(`response body`)),
						},
					},
					doGet{
						args: doGetArgs{
							req: newReq(
								withReqMethod("GET"),
								withReqUrl("https://example.com/path?b=2&a=1")),
						},
					},
				},
			},
		}...)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.ops) == 0 {
				t.Fatal("no operations specified")
			}

			synctest.Test(t, func(t *testing.T) {
				s := f()

				for i, op := range tt.ops {
					t.Logf("op %d (%T)", i, op)
					op.do(t, s)
				}
			})
		})
	}
}
