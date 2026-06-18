package httpcache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client wraps an existing [*net/http.Client] (or [http.DefaultClient]) adding caching of responses using a
// configurable [Store].
type Client struct {
	// Config is used to validate whether responses can be cached and to normalize them before storing.
	Config Config

	// HTTPClient is used for sending requests that cannot be served from the cache.
	//
	// If nil, [http.DefaultClient] is used.
	HTTPClient HTTPClient

	// Store is used to store and retrieve responses.
	Store Store
}

// HTTPClient is the interface for types that can be used to executed requests.
//
// It is implemented by [http.DefaultClient].
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func cloneHeader(h http.Header) http.Header {
	h2 := make(http.Header, len(h))
	for k, v := range h {
		h2[k] = slices.Clone(v)
	}
	return h2
}

func cloneResponse(resp *http.Response) (*http.Response, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	resp.Body = io.NopCloser(bytes.NewBuffer(body))

	return &http.Response{
		Status:        resp.Status,
		StatusCode:    resp.StatusCode,
		Proto:         resp.Proto,
		ProtoMajor:    resp.ProtoMajor,
		ProtoMinor:    resp.ProtoMinor,
		Header:        cloneHeader(resp.Header),
		Body:          io.NopCloser(bytes.NewBuffer(body)),
		ContentLength: int64(len(body)),
		Trailer:       cloneHeader(resp.Trailer),
	}, nil
}

// Do serves the given request by first trying to use a cached response if possible and otherwise falling back to
// sending an actual request and caching the returned response if possible.
//
// Requests for unsupported methods (by default anything other than GET, HEAD, and QUERY, see
// [Config.SupportedRequestMethod]) will always result in an actual request without any caching involved.
//
// The same applies to requests that include the Expect header.
//
// Errors during the parsing of request or response headers (e.g. Cache-Control) are ignored.
//
// Stale responses will result in a conditional request with If-Modified-Since and/or If-None-Match iff the cached
// response has the Last-Modified and/or ETag header set. Otherwise, the response will be sent as if no cached response
// was found.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	if !c.Config.AllowsCachedResponseFor(req) {
		return client.Do(req)
	}

	if len(req.Header["Expect"]) != 0 {
		return client.Do(req)
	}

	var reqDirectives RequestDirectives
	if s := strings.Join(req.Header["Cache-Control"], ","); s != "" {
		reqDirectives, _ = ParseRequestDirectives(s)
	}

	stored, _ := c.Store.Get(req.Context(), req)

	if stored != nil {
		age, _ := ParseAge(stored.Header.Get("Age"))

		date, _ := http.ParseTime(stored.Header.Get("Date"))

		// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-freshness-lifet
		//
		// When there is more than one value present for a given directive (e.g., two Expires header field lines or
		// multiple Cache-Control: max-age directives), either the first occurrence should be used or the response
		// should be considered stale.
		expires, _ := ParseExpires(stored.Header.Get("Expires"))

		var respDirectives ResponseDirectives
		if s := strings.Join(stored.Header["Cache-Control"], ","); s != "" {
			respDirectives, _ = ParseResponseDirectives(s)
		}

		freshnessLifetime, _ := CalculateFreshnessLifetime(
			c.Config.Private,
			date,
			expires,
			respDirectives.MaxAge,
			respDirectives.SMaxAge)

		freshness := CalculateFreshness(
			age,
			freshnessLifetime,
			reqDirectives.MinFresh,
			reqDirectives.MaxAge,
			reqDirectives.MaxStale)

		switch freshness {
		case FreshnessExpired:
			stored = nil
		case FreshnessFresh:
			return stored, nil
		case FreshnessStale:
			etag := stored.Header.Get("Etag")
			lastModified := stored.Header.Get("Last-Modified")

			// Avoid cloning request if we do not modify it
			if etag != "" || lastModified != "" {
				req = req.Clone(req.Context())

				if etag != "" {
					req.Header.Set("If-None-Match", strings.TrimPrefix(etag, "W/"))
				}

				if lastModified != "" {
					req.Header.Set("If-Modified-Since", lastModified)
				}
			} else {
				stored = nil
			}
		}
	}

	if reqDirectives.OnlyIfCached {
		return &http.Response{
			Status:        http.StatusText(http.StatusGatewayTimeout),
			StatusCode:    http.StatusGatewayTimeout,
			Proto:         req.Proto,
			ProtoMajor:    req.ProtoMajor,
			ProtoMinor:    req.ProtoMinor,
			Header:        http.Header{},
			Body:          nil,
			ContentLength: 0,
			Trailer:       http.Header{},
			Request:       req,
			TLS:           req.TLS,
		}, nil
	}

	reqTime := time.Now()

	//goland:noinspection GoResourceLeak
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	respTime := time.Now()

	if stored != nil && resp.StatusCode == http.StatusNotModified {
		return stored, nil
	}

	if c.Config.AllowsStoringResponse(resp) {
		respCopy, err := cloneResponse(resp)
		if err != nil {
			return nil, err
		}

		c.Config.RemoveUnstorableHeaders(respCopy.Header)

		_ = c.Store.Set(req.Context(), req, reqTime, respCopy, respTime)
	}

	return resp, nil
}

// Store defines the interface used by [Client] for storing and retrieving responses.
//
// A store must handle storing and retrieving requests based on their method, URL and headers specified in the Vary
// response header.
//
// A store must be safe for concurrent use by multiple goroutines.
type Store interface {
	// Get returns the stored response matching the given request.
	//
	// The given request must not be modified.
	//
	// The response must include an Age header containing the age of the response.
	Get(ctx context.Context, req *http.Request) (resp *http.Response, err error)

	// Set stores the given response in the cache.
	//
	// The given request must not be modified.
	//
	// The response body is guaranteed to be readable without errors.
	Set(
		ctx context.Context,
		req *http.Request, reqTime time.Time,
		resp *http.Response, respTime time.Time,
	) error
}

type memoryStore struct {
	entriesMu sync.RWMutex
	entries   map[string][]*memoryStoreEntry
}

type memoryStoreEntry struct {
	req      http.Request
	reqTime  time.Time
	resp     http.Response
	respBody []byte
	respTime time.Time
	vary     Vary
	varyKey  string
}

// NewMemoryStore returns a Store that stores responses in memory.
//
// This is only meant for testing.
//
// There is no limit to the number of stored responses and expired responses are never removed.
func NewMemoryStore() Store {
	return &memoryStore{}
}

func (m *memoryStore) key(req *http.Request) string {
	return fmt.Sprintf("%q %q", req.Method, req.URL.String())
}

func (m *memoryStore) Get(_ context.Context, req *http.Request) (resp *http.Response, err error) {
	key := m.key(req)

	m.entriesMu.RLock()
	defer m.entriesMu.RUnlock()

	entries := m.entries[key]

	for _, entry := range entries {
		varyKey := entry.vary.Key(nil, req.Header)

		if entry.varyKey != string(varyKey) {
			continue
		}

		return entry.restore(), nil
	}

	return nil, nil
}

func (e *memoryStoreEntry) restore() *http.Response {
	header := cloneHeader(e.resp.Header)
	header.Set("Age", strconv.Itoa(int(time.Since(e.respTime).Seconds())))

	return &http.Response{
		Status:        e.resp.Status,
		StatusCode:    e.resp.StatusCode,
		Proto:         e.resp.Proto,
		ProtoMajor:    e.resp.ProtoMajor,
		ProtoMinor:    e.resp.ProtoMinor,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(e.respBody)),
		ContentLength: int64(len(e.respBody)),
		Trailer:       cloneHeader(e.resp.Trailer),
	}
}

func (m *memoryStore) Set(
	_ context.Context,
	req *http.Request, reqTime time.Time,
	resp *http.Response, respTime time.Time,
) error {
	vary := ParseVary(resp.Header["Vary"])

	if vary.Wildcard() {
		return nil
	}

	varyKey := string(vary.Key(nil, req.Header))

	key := m.key(req)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		// TODO: Test
		return err
	}

	entry := &memoryStoreEntry{
		req:      *req,
		reqTime:  reqTime,
		resp:     *resp,
		respBody: respBody,
		respTime: respTime,
		vary:     vary,
		varyKey:  varyKey,
	}

	m.entriesMu.Lock()
	defer m.entriesMu.Unlock()

	entries := m.entries[key]

	for i := range entries {
		if entries[i].varyKey == varyKey {
			entries[i] = entry
			return nil
		}
	}

	if m.entries == nil {
		m.entries = make(map[string][]*memoryStoreEntry)
	}

	m.entries[key] = append(entries, entry)

	return nil
}
