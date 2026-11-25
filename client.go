package httpcache

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nussjustin/httpsfv"

	"github.com/nussjustin/httpcache/httpcacheutil"
)

// Client wraps an existing [*net/http.Client] (or [http.DefaultClient]) adding caching of responses using a
// configurable [Store].
type Client struct {
	// Config can be used to configure the behavior of the Client.
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

// Do serves the given request by first trying to use a cached response if possible and otherwise falling back to
// sending an actual request and caching the returned response if possible.
//
// Requests for unsupported methods (by default anything other than GET, HEAD, and QUERY, see
// [Config.SupportedRequestMethod]) will always result in an actual request without any caching involved.
//
// The same applies to requests that include the Expect header.
//
// Errors during the parsing of request or response headers (like Cache-Control) are ignored.
//
// Stale responses will result in a conditional request with If-Modified-Since and/or If-None-Match iff the cached
// response has the Last-Modified and/or ETag header set. Otherwise, the response will be sent as if no cached response
// was found.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	resp, status, err := c.do(req)
	if !c.Config.AddCacheStatus || err != nil {
		return resp, err
	}

	if text, err := status.AppendText(make([]byte, 0, 64)); err == nil {
		resp.Header.Add("Cache-Status", string(text))
	}

	return resp, nil
}

func (c *Client) do(req *http.Request) (resp *http.Response, status Status, err error) {
	defer func() {
		if resp == nil || err != nil {
			return
		}

		_ = c.invalidate(req, resp)
		_ = c.invalidateGroups(req, resp)
	}()

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	if c.Config.Name != "" {
		status.Cache = c.Config.Name
	} else {
		status.Cache = "cache"
	}

	if !c.Config.isSupportedRequestMethod(req.Method) {
		resp, err = client.Do(req)
		status.Forwarded = ForwardedReasonMethod
		return resp, status, err
	}

	if len(req.Header["Expect"]) != 0 {
		resp, err = client.Do(req)
		status.Forwarded = ForwardedReasonRequest
		return resp, status, err
	}

	reqDirectives, _ := c.Config.ParseRequestDirectives(req.Header)

	if c.Config.RespectRequestDirectiveNoCache && reqDirectives.NoCache {
		resp, err := client.Do(req)
		status.Forwarded = ForwardedReasonRequest
		return resp, status, err
	}

	reqData, err := requestDataFromRequest(req, reqDirectives)
	if err != nil {
		return nil, Status{}, err
	}

	stored, detail, err := c.Store.Get(req.Context(), reqData)

	status.Detail = detail

	if stored != nil && err == nil {
		age := stored.Age()

		date, _ := http.ParseTime(stored.Header.Get("Date"))

		var expires time.Time

		if expiresValues := stored.Header["Expires"]; len(expiresValues) > 0 {
			// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-freshness-lifet
			//
			// When there is more than one value present for a given directive (e.g., two Expires header field lines or
			// multiple Cache-Control: max-age directives), either the first occurrence should be used or the response
			// should be considered stale.
			expires, _ = ParseExpires(expiresValues[0])
		}

		respDirectives, _ := c.Config.ParseResponseDirectives(stored.Header)

		heuristicFreshnessLifetime := Opt[time.Duration]{
			Value: c.Config.HeuristicFreshnessLifetime,
			Valid: c.Config.HeuristicFreshnessLifetime > 0,
		}

		freshnessLifetime, _ := CalculateFreshnessLifetime(
			c.Config.Private,
			date,
			expires,
			respDirectives.MaxAge,
			respDirectives.SMaxAge,
			heuristicFreshnessLifetime)

		freshness := CalculateFreshness(
			age,
			freshnessLifetime,
			reqDirectives.MinFresh,
			reqDirectives.MaxAge,
			reqDirectives.MaxStale)

		status.TTL = freshnessLifetime - age

		switch freshness {
		case FreshnessExpired:
			status.Forwarded = ForwardedReasonMiss
			stored = nil
		case FreshnessFresh:
			status.Hit = true
			return stored.restore(req), status, nil
		case FreshnessStale:
			status.Forwarded = ForwardedReasonStale

			//nolint:canonicalheader
			etag := stored.Header.Get("Etag")
			lastModified := stored.Header.Get("Last-Modified")

			// Avoid cloning request if we do not modify it
			if etag != "" || lastModified != "" {
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
	} else {
		status.Forwarded = ForwardedReasonMiss
	}

	if reqDirectives.OnlyIfCached {
		resp := &http.Response{
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
		}

		return resp, status, nil
	}

	//goland:noinspection GoResourceLeak
	resp, err = client.Do(req)
	if err != nil {
		return nil, Status{}, err
	}

	if stored != nil && resp.StatusCode == http.StatusNotModified {
		status.ForwardedStatus = resp.StatusCode
		return stored.restore(req), status, nil
	}

	respDirectives, _ := c.Config.ParseResponseDirectives(resp.Header)

	// Do not read the body before we know whether we need to cache the response
	respData := responseDataFromResponseWithoutBody(resp, respDirectives)

	if c.Config.AllowsStoringResponse(reqData, respData) {
		if respData.Body, err = readBody(&resp.Body, int(resp.ContentLength)); err != nil {
			return nil, Status{}, err
		}

		respData.Header = c.Config.RemoveUnstorableHeaders(respData.Header, true)

		if detail, err := c.Store.Set(req.Context(), reqData, respData); err == nil {
			status.Detail, status.Stored = detail, true
		}
	}

	return resp, status, nil
}

func (c *Client) invalidate(req *http.Request, resp *http.Response) error {
	// 4.4 Invalidating Stored Responses
	//
	// A cache MUST invalidate the target URI (Section 7.1 of [HTTP]) when it receives a non-error status code in
	// response to an unsafe request method (including methods whose safety is unknown).
	//
	// A cache MAY invalidate other URIs when it receives a non-error status code in response to an unsafe request
	// method (including methods whose safety is unknown). In particular, the URI(s) in the Location and
	// Content-Location response header fields (if present) are candidates for invalidation; other URIs might be
	// discovered through mechanisms not specified in this document. However, a cache MUST NOT trigger an invalidation
	// under these conditions if the origin (Section 4.3.1 of [HTTP]) of the URI to be invalidated differs from that of
	// the target URI (Section 7.1 of [HTTP]). This helps prevent denial-of-service attacks.
	//
	// https://www.rfc-editor.org/rfc/rfc9111.html#name-invalidating-stored-respons

	if c.Config.IsSafeMethod(req.Method) {
		return nil
	}

	nonErrorResponse := resp.StatusCode >= 200 && resp.StatusCode <= 399

	if !nonErrorResponse {
		return nil
	}

	contentLocations := resp.Header.Values("Content-Location")
	locations := resp.Header.Values("Location")

	urls := make([]url.URL, 1, 1+len(contentLocations)+len(locations))
	urls[0] = *req.URL

	origin := httpcacheutil.MustOriginFromURL(*req.URL)

	for _, locs := range [2][]string{contentLocations, locations} {
		for _, loc := range locs {
			locURL, err := url.Parse(loc)
			if err != nil {
				continue
			}

			locOrigin, err := httpcacheutil.OriginFromURL(*locURL)
			if err != nil {
				continue
			}

			if !locOrigin.Same(origin) {
				continue
			}

			urls = append(urls, *locURL)
		}
	}

	return c.Store.Invalidate(req.Context(), urls...)
}

func (c *Client) invalidateGroups(req *http.Request, resp *http.Response) error {
	if c.Config.IsSafeMethod(req.Method) {
		return nil
	}

	groups, err := parseCacheGroups(resp.Header, "Cache-Group-Invalidation")
	if len(groups) == 0 || err != nil {
		return err
	}

	return c.Store.InvalidateGroups(req.Context(), groups...)
}

func parseCacheGroups(header http.Header, name string) ([]string, error) {
	list, err := httpsfv.ParseLines[httpsfv.List](header.Values(name))
	if err != nil {
		return nil, err
	}

	var groups []string
	if len(list.Members) > 0 {
		groups = make([]string, 0, len(list.Members))
	}

	for _, member := range list.Members {
		if t := member.Type(); t != httpsfv.ItemOrInnerListTypeItem {
			return nil, fmt.Errorf("invalid member of type %s in %s", t, name)
		}

		item := member.Item()

		if t := item.Type(); t != httpsfv.BareItemTypeString {
			return nil, fmt.Errorf("invalid item of type %s in %s", t, name)
		}

		groups = append(groups, item.String())
	}

	return groups, nil
}

// Store defines the interface used by [Client] for storing and retrieving responses.
//
// A store must handle storing and retrieving requests based on
//
//   - method, with HEAD also matching GET requests
//   - URL
//   - the No-Vary-Search header
//   - the Vary header
//
// A Store should still return an expired response, if possible, in order to support stale responses.
//
// The [github.com/nussjustin/httpcache/httpcachetest.TestStore] function can be used to test that a store
// implementation conforms to these expectations.
//
// Implementations must be safe for concurrent use by multiple goroutines.
type Store interface {
	// Get returns the stored response matching the given request.
	//
	// The returned default parameter, if not empty, is added to the Cache-Status header.
	Get(ctx context.Context, req *RequestData) (resp *ResponseData, detail string, err error)

	// Invalidate is called when one or more URLs should be invalidated as result of a non-error response to an unsafe
	// request.
	//
	// [Client] currently ignores the returned error.
	Invalidate(ctx context.Context, urls ...url.URL) error

	// InvalidateGroups is called when one or more cache groups should be invalidated as result of a non-error response
	// to an unsafe request.
	//
	// [Client] currently ignores the returned error.
	InvalidateGroups(ctx context.Context, groups ...string) error

	// Set stores the given response in the cache.
	//
	// The returned default parameter, if not empty, is added to the Cache-Status header.
	Set(ctx context.Context, req *RequestData, resp *ResponseData) (detail string, err error)
}

// RequestData contains HTTP request data that must be stored and used for lookups by a [Store].
//
// Code that receives a RequestData from somewhere else (e.g. from a [Store]) must not modify the object or retain it beyond the scope of the function call.
type RequestData struct {
	// Time is the time at which the request was sent.
	Time time.Time

	// Method contains the request method.
	Method string

	// URL contains the URL the request was sent to.
	URL *url.URL

	// Header contains the headers sent in the request.
	//
	// Certain headers may have been removed, e.g. authorization related headers.
	Header http.Header

	// Body contains the raw request body bytes or nil if the request did not send a body.
	Body []byte

	// ParsedDirectives contains the parsed Cache-Control directives.
	//
	// This can also be obtained by parsing the Cache-Control header from the Header fields and is only set so that
	// stores can avoid parsing the header again.
	ParsedDirectives RequestDirectives
}

func requestDataFromRequest(req *http.Request, reqDirectives RequestDirectives) (*RequestData, error) {
	body, err := readBody(&req.Body, int(req.ContentLength))
	if err != nil {
		return nil, err
	}

	return &RequestData{
		Time:             time.Now(),
		Method:           cmp.Or(req.Method, "GET"),
		URL:              req.URL,
		Header:           req.Header,
		Body:             body,
		ParsedDirectives: reqDirectives,
	}, nil
}

// Clone returns a deep copy of the RequestData.
func (r *RequestData) Clone() *RequestData {
	o := &RequestData{
		Time:             r.Time,
		Method:           r.Method,
		URL:              r.URL.Clone(),
		Header:           r.Header.Clone(),
		Body:             slices.Clone(r.Body),
		ParsedDirectives: r.ParsedDirectives,
	}
	o.ParsedDirectives.Extensions = slices.Clone(r.ParsedDirectives.Extensions)
	return o
}

// ResponseData contains HTTP response data that must be stored and returned by a [Store].
//
// Code that receives a ResponseData from somewhere else (e.g. from a [Store]) must not modify the object or retain it beyond the scope of the function call.
type ResponseData struct {
	// Time is the time at which the response was received by the client.
	Time time.Time

	// Status contains the status line returned for the response.
	Status string

	// StatusCode contains the status code returned for the response.
	StatusCode int

	// Header contains the headers returned for the response.
	//
	// Certain headers may have been removed, e.g. authorization related headers.
	Header http.Header

	// Body contains the raw response body bytes.
	Body []byte

	// Trailer contains the trailers returned for the response.
	Trailer http.Header

	// ParsedDirectives contains the parsed Cache-Control directives, potentially from a targeted Cache-Control header.
	//
	// This may not be set on responses returned by [Store.Get].
	ParsedDirectives ResponseDirectives
}

// Age returns the responses age based on the Date header, if present, or the Time field otherwise.
func (r *ResponseData) Age() time.Duration {
	if date, ok := r.Date(); ok {
		return time.Since(date)
	}

	return time.Since(r.Time)
}

// CacheGroups returns the cache groups specified in the Cache-Groups header as specified in RFC 9875.
//
// The groups are returned as specified including duplicates.
func (r *ResponseData) CacheGroups() ([]string, error) {
	return parseCacheGroups(r.Header, "Cache-Groups")
}

// Clone returns a deep copy of the RequestData.
func (r *ResponseData) Clone() *ResponseData {
	o := &ResponseData{
		Time:             r.Time,
		Status:           r.Status,
		StatusCode:       r.StatusCode,
		Header:           r.Header.Clone(),
		Body:             slices.Clone(r.Body),
		Trailer:          r.Trailer.Clone(),
		ParsedDirectives: r.ParsedDirectives,
	}
	o.ParsedDirectives.Extensions = slices.Clone(r.ParsedDirectives.Extensions)
	return o
}

// Date returns the parsed Date header.
func (r *ResponseData) Date() (date time.Time, ok bool) {
	dates := r.Header.Values("Date")
	if len(dates) != 1 {
		return time.Time{}, false
	}

	date, err := http.ParseTime(dates[0])
	if err != nil {
		return time.Time{}, false
	}

	return date, true
}

func (r *ResponseData) restore(req *http.Request) *http.Response {
	header := r.Header.Clone()
	header.Set("Age", strconv.Itoa(int(r.Age().Seconds())))

	var body io.ReadCloser

	if r.Body != nil {
		body = io.NopCloser(bytes.NewReader(r.Body))
	}

	return &http.Response{
		Status:        r.Status,
		StatusCode:    r.StatusCode,
		Proto:         req.Proto,
		ProtoMajor:    req.ProtoMajor,
		ProtoMinor:    req.ProtoMinor,
		Header:        header,
		Body:          body,
		ContentLength: int64(len(r.Body)),
		Trailer:       r.Trailer.Clone(),
		Request:       req,
	}
}

func responseDataFromResponseWithoutBody(resp *http.Response, respDirectives ResponseDirectives) *ResponseData {
	return &ResponseData{
		Time:             time.Now(),
		Status:           resp.Status,
		StatusCode:       resp.StatusCode,
		Header:           resp.Header,
		Trailer:          resp.Trailer,
		ParsedDirectives: respDirectives,
	}
}

func readBody(rc *io.ReadCloser, size int) (body []byte, err error) {
	if *rc == nil {
		return nil, nil
	}

	r := unwrapReader(*rc)

	switch x := r.(type) {
	case *bytes.Buffer:
		return slices.Clone(x.Bytes()), nil
	case *bytes.Reader:
		body = make([]byte, x.Len())

		if _, err = io.ReadFull(x, body); err != nil {
			panic("unreachable")
		}

		if _, err := x.Seek(-int64(len(body)), io.SeekCurrent); err != nil {
			panic("unreachable")
		}

		return body, nil
	case *strings.Reader:
		body = make([]byte, x.Len())

		if _, err = io.ReadFull(x, body); err != nil {
			panic("unreachable")
		}

		if _, err := x.Seek(-int64(len(body)), io.SeekCurrent); err != nil {
			panic("unreachable")
		}

		return body, nil
	}

	if size > 0 && size < math.MaxInt {
		body, err = readAllN(r, size)
	} else {
		body, err = io.ReadAll(r)
	}
	if err != nil {
		return nil, err
	}

	*rc = io.NopCloser(bytes.NewReader(body))

	return body, nil
}

func readAllN(r io.Reader, rn int) ([]byte, error) {
	buf := make([]byte, rn+1)

	var n int
	var err error

	for n < rn && err == nil {
		var nn int
		nn, err = r.Read(buf[n:])
		n += nn
	}

	if err == io.EOF {
		err = nil
	}

	if err != nil {
		return nil, err
	}

	if n < rn {
		return nil, fmt.Errorf("read too few bytes: got %d, want %d", n, rn)
	}

	if n > rn+1 {
		return nil, fmt.Errorf("read too many bytes: got %d, want %d", n, rn)
	}

	return buf, nil
}

func unwrapReader(rc io.ReadCloser) io.Reader {
	var r io.Reader = rc

	for {
		v := reflect.ValueOf(r)

		t := v.Type()

		if t.PkgPath() != "io" {
			return r
		}

		if n := t.Name(); n != "nopCloser" && n != "nopCloserWriterTo" {
			return r
		}

		r = v.Field(0).Interface().(io.Reader)
	}
}
