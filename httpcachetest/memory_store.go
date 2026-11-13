package httpcachetest

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/nussjustin/httpcache"
)

// MemoryStore implements a [github.com/nussjustin/httpcache.Store] that stores all responses in memory.
//
// The store has no limits on the number of stored responses and never removes expired responses.
//
// The zero value is safe for use and is equivalent to calling [NewMemoryStore] with a zero [MemoryStoreOpts].
type MemoryStore struct {
	opts MemoryStoreOpts

	entriesMu sync.RWMutex
	entries   map[string][]*memoryStoreEntry
}

// MemoryStoreOpts can be used to configure a [MemoryStore].
//
// The zero value is a valid configuration and should be good enough for most cases.
type MemoryStoreOpts struct {
	// Private should be set to true if the cache is considered private.
	//
	// This is used when calculating freshness for expired items. See [MemoryStoreOpts.ReturnNilForExpiredResponses].
	Private bool

	// ReturnNilForExpiredResponses, if true, causes [MemoryStore.Get] to return (nil, nil) once a response is expired
	// for at least ReturnNilForExpiredResponsesAfter.
	ReturnNilForExpiredResponses bool

	// ReturnNilForExpiredResponsesAfter is the duration after which an expired response is no longer returned.
	ReturnNilForExpiredResponsesAfter time.Duration

	// ValidateNoVarySearch, if true, causes [MemoryStore.Set] to return an error when the response contains an invalid
	// No-Vary-Search header.
	//
	// By default, the store will ignore an invalid header instead.
	ValidateNoVarySearch bool
}

// NewMemoryStore returns a new [MemoryStore] using the given options.
func NewMemoryStore(opts MemoryStoreOpts) *MemoryStore {
	return &MemoryStore{opts: opts}
}

type memoryStoreEntry struct {
	req                http.Request
	reqTime            time.Time
	resp               http.Response
	respBody           []byte
	respTime           time.Time
	urlVariationConfig httpcache.URLVariationConfig
	vary               httpcache.Vary
}

func (m *MemoryStore) key(req *http.Request) string {
	method := req.Method
	if req.Method == "HEAD" {
		method = "GET"
	}
	// The query is handled by the URLVariationCOnfig
	urlWithoutQuery := *req.URL
	urlWithoutQuery.RawQuery = ""
	return method + " " + urlWithoutQuery.String()
}

func (m *MemoryStore) Get(_ context.Context, req *http.Request) (resp *http.Response, err error) {
	key := m.key(req)

	m.entriesMu.RLock()
	defer m.entriesMu.RUnlock()

	entries := m.entries[key]

	for _, entry := range entries {
		if !entry.urlVariationConfig.Equals(entry.req.URL, req.URL) {
			continue
		}

		if !entry.vary.Equals(entry.req.Header, req.Header) {
			continue
		}

		if !m.shouldReturnExpired(entry) {
			return nil, nil
		}

		return entry.restore(), nil
	}

	return nil, nil
}

func (e *memoryStoreEntry) age() time.Duration {
	now := time.Now()

	var age httpcache.Opt[time.Duration]
	if date, err := http.ParseTime(e.resp.Header.Get("Date")); err == nil {
		age.Value, age.Valid = now.Sub(date), true
	}

	return httpcache.CalculateAge(
		now,
		e.reqTime,
		age,
		e.respTime,
		e.respTime)
}

func (e *memoryStoreEntry) restore() *http.Response {
	header := e.resp.Header.Clone()
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
		Trailer:       e.resp.Trailer.Clone(),
	}
}

func (m *MemoryStore) shouldReturnExpired(e *memoryStoreEntry) bool {
	if !m.opts.ReturnNilForExpiredResponses {
		return true
	}

	age := e.age()
	date, _ := http.ParseTime(e.resp.Header.Get("Date"))
	expires, _ := httpcache.ParseExpires(e.resp.Header.Get("Expires"))

	respDirectives, _ := httpcache.ParseResponseDirectives(e.resp.Header.Get("Cache-Control"))

	freshnessLifetime, _ := httpcache.CalculateFreshnessLifetime(
		m.opts.Private,
		date,
		expires,
		respDirectives.MaxAge,
		respDirectives.SMaxAge)

	var minFresh, maxAge, maxStale httpcache.Opt[time.Duration]

	maxAge.Value, maxAge.Valid = freshnessLifetime+m.opts.ReturnNilForExpiredResponsesAfter-1, true

	freshness := httpcache.CalculateFreshness(
		age,
		freshnessLifetime,
		minFresh,
		maxAge,
		maxStale)

	return freshness != httpcache.FreshnessExpired
}

func (m *MemoryStore) Set(
	_ context.Context,
	req *http.Request, reqTime time.Time,
	resp *http.Response, respTime time.Time,
) error {
	vary := httpcache.ParseVary(resp.Header["Vary"])

	if vary.Wildcard() {
		return nil
	}

	urlVariationConfig, err := httpcache.ParseNoVarySearch(resp.Header.Values("No-Vary-Search"))
	if err != nil && m.opts.ValidateNoVarySearch {
		return err
	}

	key := m.key(req)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	entry := &memoryStoreEntry{
		req:                *req,
		reqTime:            reqTime,
		resp:               *resp,
		respBody:           respBody,
		respTime:           respTime,
		urlVariationConfig: urlVariationConfig,
		vary:               vary,
	}

	// Note: Use URL.Clone once Go 1.27 is required.
	entry.req.URL = new(*req.URL)
	if entry.req.URL.User != nil {
		entry.req.URL.User = new(*entry.req.URL.User)
	}

	m.entriesMu.Lock()
	defer m.entriesMu.Unlock()

	entries := m.entries[key]

	for i := range entries {
		if vary.Equals(entries[i].req.Header, entry.req.Header) {
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
