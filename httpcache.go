// Package httpcache implements functions related to HTTP caching based on RFC 9111.
package httpcache

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nussjustin/httpcache/internal/cachecontrol"
)

// Config defines characteristics of the cache based on which cacheability can be calculated.
type Config struct {
	// CacheableByExtension can be used to mark a response as cacheable even if it does not match any of the other
	// criteria specified in RFC 9111.
	//
	// If nil, only the criteria from RFC 9111 is applied to determine cacheability.
	CacheableByExtension func(Request, Response) bool

	// CanUnderstandResponseCode is used to check if a response with status code 206 or 304, or with must-understand cache
	// directive should be cached.
	//
	// If nil, such responses are not cached.
	CanUnderstandResponseCode func(code int) bool

	// IgnoreRequestDirectiveNoStore can be set to disable checking of the no-store Cache-Control request directive.
	//
	// Note that while RFC 9111 specifies that the no-store directive should prevent responses from being cached, the
	// steps for determining whether a response can be stored do not actually say anything about the directive.
	//
	// The [Config.CanStore] method by default respects the directive, but caches may want to ignore it.
	IgnoreRequestDirectiveNoStore bool

	// IsHeuristicallyCacheableStatusCode is called to check if a status code can be cached without explicit opt-in via
	// cache directives.
	//
	// If nil, the status codes from [HeuristicallyCacheableStatusCodes] are allowed.
	IsHeuristicallyCacheableStatusCode func(code int) bool

	// Private configures the cache to be private, as understood by RFC 9111.
	Private bool

	// RespectPrivateHeaders can be set to true to enable caches to be stored even when private is specified in the
	// response, as long as the private directive has specified at least one header in its value.
	//
	// It also causes [Config.RemoveUnstorableHeaders] to remove headers specified for the "private" response directive,
	// but not for the "no-cache" directive (as those are still usable depending on the request).
	//
	// If false, the directive is treated as if it had no value.
	RespectPrivateHeaders bool

	// StoreProxyHeaders, if set, causes [Config.RemoveUnstorableHeaders] to not remove the following headers:
	//
	// - Proxy-Authenticate
	// - Proxy-Authentication-Info
	// - Proxy-Authorization
	StoreProxyHeaders bool

	// SupportedRequestMethod is called to check if the request method can be cached,
	//
	// If nil, only GET, HEAD and QUERY methods can be cached.
	SupportedRequestMethod func(method string) bool
}

// HeuristicallyCacheableStatusCodes contains HTTP response codes specified in RFC 9110 that are allowed to be cached
// by default.
var HeuristicallyCacheableStatusCodes = []int{
	http.StatusOK,
	http.StatusNonAuthoritativeInfo,
	http.StatusNoContent,
	http.StatusPartialContent,
	http.StatusMultipleChoices,
	http.StatusMovedPermanently,
	http.StatusPermanentRedirect,
	http.StatusNotFound,
	http.StatusMethodNotAllowed,
	http.StatusGone,
	http.StatusRequestURITooLong,
	http.StatusNotImplemented,
}

// CanStore checks if a response can be cached and for how long.
func (c Config) CanStore(req Request, resp Response) bool {
	// 3. Storing Responses in Caches
	//
	// A cache MUST NOT store a response to a request unless:

	// - the request method is understood by the cache;
	if !c.supportedRequestMethod(req.Method) {
		return false
	}

	// - the response status code is final (see Section 15 of [HTTP]);
	if resp.StatusCode < 200 {
		return false
	}

	respDirectives, _ := resp.Directives()

	// - if the response status code is 206 or 304, or the must-understand cache directive (see Section 5.2.2.3) is
	//   present: the cache understands the response status code
	if resp.StatusCode == http.StatusPartialContent || resp.StatusCode == http.StatusNotModified || respDirectives.MustUnderstand {
		if !c.canUnderstandResponseCode(resp.StatusCode) {
			return false
		}
	}

	// - the no-store cache directive is not present in the response (see Section 5.2.2.5);
	if respDirectives.NoStore {
		return false
	}

	// - if the cache is shared: the private response directive is either not present or allows a shared cache to store
	//   a modified response; see Section 5.2.2.7);
	if !c.Private && respDirectives.Private && (!c.RespectPrivateHeaders || len(respDirectives.PrivateHeaders) == 0) {
		return false
	}

	// - if the cache is shared: the Authorization header field is not present in the request (see Section 11.6.2 of
	//   [HTTP]) or a response directive is present that explicitly allows shared caching (see Section 3.5); and
	if !c.Private && req.Authorized() && !respDirectives.MustRevalidate && !respDirectives.Public && respDirectives.SMaxAge <= 0 {
		return false
	}

	respExpires, _ := resp.Expires()

	// - the response contains at least one of the following
	switch {
	// a public response directive (see Section 5.2.2.9);
	case respDirectives.Public:
	// a private response directive, if the cache is not shared (see Section 5.2.2.7);
	case c.Private && respDirectives.Private:
	// an Expires header field (see Section 5.3);
	case !respExpires.IsZero():
	// a max-age response directive (see Section 5.2.2.1);
	case respDirectives.MaxAge > 0:
	// if the cache is shared: an s-maxage response directive (see Section 5.2.2.10);
	case !c.Private && respDirectives.SMaxAge > 0:
	// a cache extension that allows it to be cached (see Section 5.2.3); or
	case c.cacheableByExtension(req, resp):
	// a status code that is defined as heuristically cacheable (see Section 4.2.2).
	case c.isHeuristicallyCacheableStatusCode(resp.StatusCode):
	default:
		return false
	}

	if !c.IgnoreRequestDirectiveNoStore {
		reqDirectives, _ := req.Directives()

		// Note: This is not actually part of "3. Storing Responses in Caches".
		if reqDirectives.NoStore {
			return false
		}
	}

	return true
}

func (c Config) cacheableByExtension(req Request, resp Response) bool {
	if c.CacheableByExtension == nil {
		return false
	}

	return c.CacheableByExtension(req, resp)
}

func (c Config) canUnderstandResponseCode(code int) bool {
	if c.CanUnderstandResponseCode == nil {
		return false
	}

	return c.CanUnderstandResponseCode(code)
}

func (c Config) isHeuristicallyCacheableStatusCode(code int) bool {
	if c.IsHeuristicallyCacheableStatusCode == nil {
		return slices.Contains(HeuristicallyCacheableStatusCodes, code)
	}

	return c.IsHeuristicallyCacheableStatusCode(code)
}

func (c Config) supportedRequestMethod(method string) bool {
	if c.SupportedRequestMethod == nil {
		return method == "GET" || method == "HEAD" || method == "QUERY"
	}

	return c.SupportedRequestMethod(method)
}

// RemoveUnstorableHeaders removes response headers that must not be stored.
func (c Config) RemoveUnstorableHeaders(headers http.Header) {
	//  3.1. Storing Header and Trailer Fields
	//
	// Caches MUST include all received response header fields -- including unrecognized ones -- when storing a response;
	// this assures that new HTTP header fields can be successfully deployed. However, the following exceptions are made:

	connection := headers["Connection"]

	// The Connection header field and fields whose names are listed in it are required by Section 7.6.1 of [HTTP] to be
	// removed before forwarding the message. This MAY be implemented by doing so before storage.
	delete(headers, "Connection")

	// Likewise, some fields' semantics require them to be removed before forwarding the message, and this MAY be
	// implemented by doing so before storage; see Section 7.6.1 of [HTTP] for some examples.
	for _, tokens := range connection {
		for header := range strings.FieldsSeq(tokens) {
			delete(headers, http.CanonicalHeaderKey(header))
		}
	}

	if c.RespectPrivateHeaders && len(headers["Cache-Control"]) > 0 {
		// The no-cache (Section 5.2.2.4) and private (Section 5.2.2.7) cache directives can have arguments that prevent
		// storage of header fields by all caches and shared caches, respectively.

		directives, _ := ParseResponseDirectives(strings.Join(headers["Cache-Control"], ","))

		for _, header := range directives.PrivateHeaders {
			delete(headers, http.CanonicalHeaderKey(header))
		}
	}

	if !c.StoreProxyHeaders {
		// Header fields that are specific to the proxy that a cache uses when forwarding a request MUST NOT be stored,
		// unless the cache incorporates the identity of the proxy into the cache key. Effectively, this is limited to
		// Proxy-Authenticate (Section 11.7.1 of [HTTP]), Proxy-Authentication-Info (Section 11.7.3 of [HTTP]), and
		// Proxy-Authorization (Section 11.7.2 of [HTTP]).
		delete(headers, "Proxy-Authenticate")
		delete(headers, "Proxy-Authentication-Info")
		delete(headers, "Proxy-Authorization")
	}
}

// Request contains HTTP request information related to caching. It can be used with [Config] to check if
// a request is cacheable or not.
type Request struct {
	// Method contains the HTTP method of the request.
	Method string

	// URL is the requested URL.
	URL *url.URL

	// Header contains the request headers.
	Header http.Header
}

// Authorized returns true if the request contains the Authorization header.
func (r Request) Authorized() bool {
	return len(r.Header["Authorization"]) != 0
}

// Directives returns the parsed Cache-Control header for this request.
func (r Request) Directives() (RequestDirectives, error) {
	return ParseRequestDirectives(strings.Join(r.Header["Cache-Control"], ", "))
}

// Response contains HTTP response information related to caching. It can be used with [Config] to check if
// a response is cacheable or not.
type Response struct {
	// StatusCode is the final HTTP response code used for the response.
	StatusCode int

	// Header contains the response headers.
	Header http.Header

	// Trailer contains the response trailers.
	Trailer http.Header
}

// Age returns the response age in seconds.
func (r Response) Age() (time.Duration, error) {
	ss := r.Header["Age"]
	if len(ss) == 0 {
		return 0, nil
	}
	return ParseAge(strings.Join(ss, ", "))
}

// Directives returns the parsed Cache-Control header for this response.
func (r Response) Directives() (ResponseDirectives, error) {
	return ParseResponseDirectives(strings.Join(r.Header["Cache-Control"], ", "))
}

// Expires returns the time at which the response expires, if any.
func (r Response) Expires() (time.Time, error) {
	ss := r.Header["Expires"]
	if len(ss) == 0 {
		return time.Time{}, nil
	}
	return ParseExpires(strings.Join(ss, ", "))
}

// Vary returns the parsed header names from the Vary header.
//
// Duplicates are removed and the result is sorted.
func (r Response) Vary() []string {
	var headers []string

	for _, vary := range r.Header["Vary"] {
		for header := range strings.SplitSeq(vary, ",") {
			header = strings.TrimSpace(header)
			header = textproto.CanonicalMIMEHeaderKey(header)

			headers = append(headers, header)
		}
	}

	if len(headers) == 0 {
		return nil
	}

	slices.Sort(headers)

	last := 0

	for i := range len(headers) {
		if headers[i] == headers[last] {
			continue
		}

		headers[last+1] = headers[i]
		last++
	}

	sorted := headers[: last+1 : last+1]

	clear(headers[len(sorted):])

	return sorted
}

// ParseAge parses a duration in seconds from the HTTP Age header.
func ParseAge(s string) (time.Duration, error) {
	d, err := parseDeltaSeconds(s)
	if err != nil {
		return 0, err
	}
	if int64(d) < 0 || int64(d) > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64), nil
	}
	return time.Duration(d) * time.Second, nil
}

var (
	errEmptyDeltaSeconds   = errors.New("empty value for delta-seconds")
	errInvalidDeltaSeconds = errors.New("invalid value for delta-seconds")
)

func parseDeltaSeconds(s string) (uint64, error) {
	var d uint64

	if s == "" {
		return 0, errEmptyDeltaSeconds
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errInvalidDeltaSeconds
		}

		d1 := d
		d1 *= 10
		d1 += uint64(r - '0')

		if d1 < d {
			// From https://www.rfc-editor.org/rfc/rfc9111#section-1.2.2:
			//
			// If a cache receives a delta-seconds value greater than the greatest integer it can represent, or if any
			// of its subsequent calculations overflows, the cache MUST consider the value to be 2147483648 (2^31) or
			// the greatest positive integer it can conveniently represent.
			//
			d1 = math.MaxUint64
		}

		d = d1
	}

	return d, nil
}

// ParseExpires parses a time and date from the HTTP Expires header.
func ParseExpires(s string) (time.Time, error) {
	return time.Parse("Mon, 02 Jan 2006 15:04:05 GMT", s)
}

// ExtensionDirective represents a non-standard Cache-Control directive.
type ExtensionDirective struct {
	// Name of the directive. May be empty if HasValue is true.
	Name string

	// Value of the directive, if any. May be empty. CanStore HasValue to differentiate between an empty and no value.
	Value string

	// HasValue is true if Value is set.
	HasValue bool
}

// String implements the [fmt.Stringer] interface.
func (e ExtensionDirective) String() string {
	if e.HasValue {
		return e.Name + `="` + e.Value + `"`
	}
	return e.Name
}

// RequestDirectives contains parsed cache directives from a Cache-Control header for a request.
type RequestDirectives struct {
	// https://www.rfc-editor.org/rfc/rfc9111#name-max-age
	MaxAge time.Duration

	// https://www.rfc-editor.org/rfc/rfc9111#name-max-stale
	MaxStale time.Duration

	// https://www.rfc-editor.org/rfc/rfc9111#name-min-fresh
	MinFresh time.Duration

	// https://www.rfc-editor.org/rfc/rfc9111#name-no-cache
	NoCache bool

	// https://www.rfc-editor.org/rfc/rfc9111#name-no-store
	NoStore bool

	// https://www.rfc-editor.org/rfc/rfc9111#name-no-transform
	NoTransform bool

	// https://www.rfc-editor.org/rfc/rfc9111#name-only-if-cached
	OnlyIfCached bool

	// Extensions contains all non-standard directives in the order encountered.
	//
	// The directive names are always lower cased.
	//
	// If the parsed header contained duplicate directives, this slice will also contain these duplicate directives.
	Extensions []ExtensionDirective
}

// ParseRequestDirectives parses a Cache-Control request header and returns a struct of the parsed directives.
//
// Any errors during parsing are collected and returned as one using [errors.Join] together with the struct containing
// all parseable data.
func ParseRequestDirectives(header string) (RequestDirectives, error) {
	var c RequestDirectives
	var errs []error

	for d := range cachecontrol.Parse(header) {
		name := strings.ToLower(d.Name)

		switch name {
		case "max-age":
			dur, err := ParseAge(d.Value)
			if err != nil {
				errs = append(errs, fmt.Errorf("invalid value for max-age: %w", err))
				break
			}
			c.MaxAge = dur
		case "max-stale":
			dur, err := ParseAge(d.Value)
			if err != nil {
				errs = append(errs, fmt.Errorf("invalid value for max-stale: %w", err))
				break
			}
			c.MaxStale = dur
		case "min-fresh":
			dur, err := ParseAge(d.Value)
			if err != nil {
				errs = append(errs, fmt.Errorf("invalid value for min-fresh: %w", err))
				break
			}
			c.MinFresh = dur
		case "no-cache":
			c.NoCache = true
		case "no-store":
			c.NoStore = true
		case "no-transform":
			c.NoTransform = true
		case "only-if-cached":
			c.OnlyIfCached = true
		default:
			c.Extensions = append(c.Extensions, ExtensionDirective(d))
		}
	}

	var err error

	if len(errs) > 0 {
		err = errors.Join(errs...)
	}

	return c, err
}

// String implements the [fmt.Stringer] interface.
func (d RequestDirectives) String() string {
	ss := make([]string, 0, 16)
	if d.MaxAge > 0 {
		ss = append(ss, "max-age="+strconv.Itoa(int(d.MaxAge/time.Second)))
	}
	if d.MaxStale > 0 {
		ss = append(ss, "max-stale="+strconv.Itoa(int(d.MaxStale/time.Second)))
	}
	if d.MinFresh > 0 {
		ss = append(ss, "min-fresh="+strconv.Itoa(int(d.MinFresh/time.Second)))
	}
	if d.NoCache {
		ss = append(ss, "no-cache")
	}
	if d.NoStore {
		ss = append(ss, "no-store")
	}
	if d.NoTransform {
		ss = append(ss, "no-transform")
	}
	if d.OnlyIfCached {
		ss = append(ss, "only-if-cached")
	}
	for _, ext := range d.Extensions {
		ss = append(ss, ext.String())
	}
	return strings.Join(ss, ", ")
}

// ResponseDirectives contains parsed cache directives from a Cache-Control header for a response.
type ResponseDirectives struct {
	// https://www.rfc-editor.org/rfc/rfc9111#name-max-age-2
	MaxAge time.Duration

	// https://www.rfc-editor.org/rfc/rfc9111#name-must-revalidate
	MustRevalidate bool

	// https://www.rfc-editor.org/rfc/rfc9111#name-must-understand
	MustUnderstand bool

	// https://www.rfc-editor.org/rfc/rfc9111#name-no-cache-2
	NoCache bool

	// NoCacheHeaders contains the header names set via the no-cache directive, when the directive has a value.
	//
	// If the last no-cache directive had no value, this will be nil. Otherwise, the slice will be non-nil, even if empty.
	//
	// https://www.rfc-editor.org/rfc/rfc9111#name-no-cache-2
	NoCacheHeaders []string

	// https://www.rfc-editor.org/rfc/rfc9111#name-no-store-2
	NoStore bool

	// https://www.rfc-editor.org/rfc/rfc9111#name-no-transform-2
	NoTransform bool

	// https://www.rfc-editor.org/rfc/rfc9111#name-private
	Private bool

	// https contains the header names set via the private a directive, when the directive has a value.
	//
	// If the last private directive had no value, this will be nil. Otherwise, the slice will be non-nil, even if empty.
	//
	// https://www.rfc-editor.org/rfc/rfc9111#name-no-cache-2
	PrivateHeaders []string

	// https://www.rfc-editor.org/rfc/rfc9111#name-proxy-revalidate
	ProxyRevalidate bool

	// https://www.rfc-editor.org/rfc/rfc9111#name-public
	Public bool

	// https://www.rfc-editor.org/rfc/rfc9111#name-s-maxage
	SMaxAge time.Duration

	// Extensions contains all non-standard directives in the order encountered.
	//
	// The directive names are always lower cased.
	//
	// If the parsed header contained duplicate directives, this slice will also contain these duplicate directives.
	Extensions []ExtensionDirective
}

// ParseResponseDirectives parses a Cache-Control response header and returns a struct of the parsed directives.
//
// Any errors during parsing are collected and returned as one using [errors.Join] together with the struct containing
// all parseable data.
func ParseResponseDirectives(header string) (ResponseDirectives, error) {
	var c ResponseDirectives
	var errs []error

	for d := range cachecontrol.Parse(header) {
		name := strings.ToLower(d.Name)

		switch name {
		case "max-age":
			dur, err := ParseAge(d.Value)
			if err != nil {
				errs = append(errs, fmt.Errorf("invalid value for max-age: %w", err))
				break
			}
			c.MaxAge = dur
		case "must-revalidate":
			c.MustRevalidate = true
		case "must-understand":
			c.MustUnderstand = true
		case "no-cache":
			c.NoCache = true

			if d.HasValue {
				c.NoCacheHeaders = strings.Fields(d.Value)
			} else {
				c.NoCacheHeaders = nil
			}
		case "no-store":
			c.NoStore = true
		case "no-transform":
			c.NoTransform = true
		case "private":
			c.Private = true

			if d.HasValue {
				c.PrivateHeaders = strings.Fields(d.Value)
			} else {
				c.PrivateHeaders = nil
			}
		case "proxy-revalidate":
			c.ProxyRevalidate = true
		case "public":
			c.Public = true
		case "s-maxage":
			dur, err := ParseAge(d.Value)
			if err != nil {
				errs = append(errs, fmt.Errorf("invalid value for s-maxage: %w", err))
				break
			}
			c.SMaxAge = dur
		default:
			c.Extensions = append(c.Extensions, ExtensionDirective(d))
		}
	}

	var err error

	if len(errs) > 0 {
		err = errors.Join(errs...)
	}

	return c, err
}

// String implements the [fmt.Stringer] interface.
func (d ResponseDirectives) String() string {
	ss := make([]string, 0, 16)
	if d.MaxAge > 0 {
		ss = append(ss, "max-age="+strconv.Itoa(int(d.MaxAge/time.Second)))
	}
	if d.MustRevalidate {
		ss = append(ss, "must-revalidate")
	}
	if d.MustUnderstand {
		ss = append(ss, "must-understand")
	}
	if d.NoCache {
		if len(d.NoCacheHeaders) > 0 {
			// Note: The quoted-string form is required, even if technically not necessary
			ss = append(ss, `no-cache="`+strings.Join(d.NoCacheHeaders, ` `)+`"`)
		} else {
			ss = append(ss, "no-cache")
		}
	}
	if d.NoStore {
		ss = append(ss, "no-store")
	}
	if d.NoTransform {
		ss = append(ss, "no-transform")
	}
	if d.Private {
		if len(d.PrivateHeaders) > 0 {
			// Note: The quoted-string form is required, even if technically not necessary
			ss = append(ss, `private="`+strings.Join(d.PrivateHeaders, ` `)+`"`)
		} else {
			ss = append(ss, "private")
		}
	}
	if d.ProxyRevalidate {
		ss = append(ss, "proxy-revalidate")
	}
	if d.Public {
		ss = append(ss, "public")
	}
	if d.SMaxAge > 0 {
		ss = append(ss, "s-maxage="+strconv.Itoa(int(d.SMaxAge/time.Second)))
	}
	for _, ext := range d.Extensions {
		ss = append(ss, ext.String())
	}
	return strings.Join(ss, ", ")
}
