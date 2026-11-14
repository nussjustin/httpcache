package httpcachetest

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/maphash"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/nussjustin/httpcache"
	"github.com/nussjustin/httpcache/httpcacheutil"
)

// MemoryStore implements a [github.com/nussjustin/httpcache.Store] that stores all responses in memory.
//
// The store has no limits on the number of stored responses and never removes expired responses, unless explicitly
// requested by calling [MemoryStore.Prune].
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
	// HeuristicFreshnessLifetime is the default freshness lifetime used when no max-age/s-maxage Cache-Control
	// directives or Expire are set and is used for checking if a response is expired.
	HeuristicFreshnessLifetime time.Duration

	// Private should be set to true if the cache is considered private.
	//
	// This is used when calculating freshness for expired items. See [MemoryStoreOpts.ReturnNilForExpiredResponses].
	Private bool

	// ReturnDetailString enables returning details in [MemoryStore.Get] and [MemoryStore.Set] for use in the
	// Cache-Status header.
	ReturnDetailString bool

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

var _ httpcache.Store = (*MemoryStore)(nil)

// NewMemoryStore returns a new [MemoryStore] using the given options.
func NewMemoryStore(opts MemoryStoreOpts) *MemoryStore {
	return &MemoryStore{opts: opts}
}

type memoryStoreEntry struct {
	req                httpcache.RequestData
	resp               httpcache.ResponseData
	urlVariationConfig httpcache.URLVariationConfig
	vary               httpcache.Vary
}

func (e *memoryStoreEntry) age() time.Duration {
	now := time.Now()

	var age httpcache.Opt[time.Duration]
	if date, err := http.ParseTime(e.resp.Header.Get("Date")); err == nil {
		age.Value, age.Valid = now.Sub(date), true
	}

	return httpcache.CalculateAge(
		now,
		e.req.Time,
		age,
		e.resp.Time,
		e.resp.Time)
}

func (e *memoryStoreEntry) freshness(
	privateCache bool,
	extraMaxAge time.Duration,
	heuristicFreshnessLifetime time.Duration,
) httpcache.Freshness {
	age := e.age()
	date, _ := http.ParseTime(e.resp.Header.Get("Date"))
	expires, _ := httpcache.ParseExpires(e.resp.Header.Get("Expires"))

	respDirectives, _ := httpcache.ParseResponseDirectives(e.resp.Header.Get("Cache-Control"))

	freshnessLifetime, _ := httpcache.CalculateFreshnessLifetime(
		privateCache,
		date,
		expires,
		respDirectives.MaxAge,
		respDirectives.SMaxAge,
		httpcache.Opt[time.Duration]{
			Value: heuristicFreshnessLifetime,
			Valid: heuristicFreshnessLifetime > 0,
		},
	)

	var minFresh, maxAge, maxStale httpcache.Opt[time.Duration]

	maxAge.Value, maxAge.Valid = freshnessLifetime+extraMaxAge-1, true

	return httpcache.CalculateFreshness(
		age,
		freshnessLifetime,
		minFresh,
		maxAge,
		maxStale)
}

var seed = maphash.MakeSeed()

func (m *MemoryStore) detail(s string) string {
	if m.opts.ReturnDetailString {
		return s
	}
	return ""
}

func (m *MemoryStore) key(req httpcache.RequestData) string {
	var h maphash.Hash
	h.SetSeed(seed)

	if _, err := httpcacheutil.HashRequest(&h, req.Method, req.URL, req.Body); err != nil {
		panic(fmt.Errorf("HashRequest returned an error: %w", err))
	}

	var b1 [8]byte
	binary.BigEndian.PutUint64(b1[:], h.Sum64())

	var b2 [8 * 2]byte
	hex.Encode(b2[:], b1[:])

	return string(b2[:])
}

// Clear clears the store, removing all stored responses.
func (m *MemoryStore) Clear() {
	m.entriesMu.Lock()
	defer m.entriesMu.Unlock()

	clear(m.entries)
}

// Get implements the [github.com/nussjustin/httpcache.Store.Get] method.
func (m *MemoryStore) Get(_ context.Context, req httpcache.RequestData) (resp *httpcache.ResponseData, detail string, err error) {
	key := m.key(req)

	m.entriesMu.RLock()
	defer m.entriesMu.RUnlock()

	entries := m.entries[key]

	for _, entry := range entries {
		if !entry.urlVariationConfig.Equals(&entry.req.URL, &req.URL) {
			continue
		}

		if !entry.vary.Equals(entry.req.Header, req.Header) {
			continue
		}

		if m.opts.ReturnNilForExpiredResponses {
			freshness := entry.freshness(
				m.opts.Private,
				m.opts.ReturnNilForExpiredResponsesAfter,
				m.opts.HeuristicFreshnessLifetime,
			)

			if freshness == httpcache.FreshnessExpired {
				return nil, m.detail("expired"), nil
			}
		}

		return &entry.resp, "", nil
	}

	return nil, "", nil
}

// Prune removes all expired responses from the store.
func (m *MemoryStore) Prune() {
	m.entriesMu.Lock()
	defer m.entriesMu.Unlock()

	for key, entries := range m.entries {
		m.entries[key] = slices.DeleteFunc(entries, func(entry *memoryStoreEntry) bool {
			freshness := entry.freshness(m.opts.Private, 0, m.opts.HeuristicFreshnessLifetime)

			return freshness == httpcache.FreshnessExpired
		})
	}

	maps.DeleteFunc(m.entries, func(_ string, entries []*memoryStoreEntry) bool {
		return len(entries) == 0
	})
}

// Set implements the [github.com/nussjustin/httpcache.Store.Set] method.
func (m *MemoryStore) Set(_ context.Context, req httpcache.RequestData, resp httpcache.ResponseData) (detail string, err error) {
	vary := httpcache.ParseVary(resp.Header["Vary"])

	if vary.Wildcard() {
		return m.detail("Vary wildcard"), nil
	}

	urlVariationConfig, err := httpcache.ParseNoVarySearch(resp.Header.Values("No-Vary-Search"))
	if err != nil && m.opts.ValidateNoVarySearch {
		return m.detail("invalid No-Vary-Search"), err
	}

	key := m.key(req)

	entry := &memoryStoreEntry{
		req:                req,
		resp:               resp,
		urlVariationConfig: urlVariationConfig,
		vary:               vary,
	}

	m.entriesMu.Lock()
	defer m.entriesMu.Unlock()

	entries := m.entries[key]

	for i := range entries {
		if vary.Equals(entries[i].req.Header, entry.req.Header) {
			entries[i] = entry
			return "", nil
		}
	}

	if m.entries == nil {
		m.entries = make(map[string][]*memoryStoreEntry)
	}

	m.entries[key] = append(entries, entry)

	return "", nil
}
