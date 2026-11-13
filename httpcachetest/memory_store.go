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
// The zero value is safe for use.
type MemoryStore struct {
	entriesMu sync.RWMutex
	entries   map[string][]*memoryStoreEntry
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

		return entry.restore(), nil
	}

	return nil, nil
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
	if err != nil {
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
