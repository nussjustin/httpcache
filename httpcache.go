// Package httpcache implements functions related to HTTP caching based on RFC 9111.
package httpcache

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nussjustin/httpsfv"

	"github.com/nussjustin/httpcache/internal/cachecontrol"
)

// CalculateAge calculates the age for a response as defined in RFC 9111, Section 4.2.3.
//
// reqTime must contain the local time the request was sent.
// respTime must contain the local time the response was received.
func CalculateAge(
	now time.Time,
	reqTime time.Time,
	respAge Opt[time.Duration],
	respDate time.Time,
	respTime time.Time,
) time.Duration {
	// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-age
	//
	// A response's age can be calculated in two entirely independent ways:

	// 1. the "apparent_age": response_time minus date_value, if the implementation's clock is reasonably well
	// synchronized to the origin server's clock. If the result is negative, the result is replaced by zero.
	apparentAge := max(0, respTime.Sub(respDate))

	// 2. the "corrected_age_value", if all of the caches along the response path implement HTTP/1.1 or greater. A cache
	// MUST interpret this value relative to the time the request was initiated, not the time that the response was
	// received.
	responseDelay := respTime.Sub(reqTime)
	correctedAgeValue := respAge.Value + responseDelay

	// The corrected_age_value MAY be used as the corrected_initial_age.
	correctedInitialAge := correctedAgeValue

	// In circumstances where very old cache implementations that might not correctly insert Age are present, ...
	if !respAge.Valid {
		// ... corrected_initial_age can be calculated more conservatively as
		correctedInitialAge = max(apparentAge, correctedAgeValue)
	}

	// The current_age of a stored response can then be calculated by adding the time (in seconds) since the stored
	// response was last validated by the origin server to the corrected_initial_age.
	residentTime := now.Sub(respTime)
	currentAge := correctedInitialAge + residentTime

	return currentAge
}

// Freshness is an enumeration of possible freshness states.
type Freshness uint

const (
	// FreshnessExpired is used for responses that are expired and can no longer be used.
	FreshnessExpired Freshness = iota

	// FreshnessFresh is used for responses that are fresh and can be reused.
	FreshnessFresh

	// FreshnessStale is used for responses that are no longer fresh but may still be used.
	FreshnessStale
)

// CalculateFreshness calculates the freshness of a response.
//
// The freshnessLifetime can be calculated using [ResponseMetadata.FreshnessLifetime].
//
// It does not consider directives like no-cache or must-revalidate which influence whether a (stale) response can be
// re-used.
func CalculateFreshness(
	currentAge time.Duration,
	freshnessLifetime time.Duration,
	minFresh Opt[time.Duration],
	maxAge Opt[time.Duration],
	maxStale Opt[time.Duration],
) Freshness {
	// From https://www.rfc-editor.org/rfc/rfc9111#name-freshness
	//
	// The calculation to determine if a response is fresh is:
	responseIsFresh := freshnessLifetime > currentAge

	// Clients can send the max-age or min-fresh request directives (Section 5.2.1) to suggest limits on the freshness
	// calculations for the corresponding response. However, caches are not required to honor them.
	if maxAge.Valid && maxAge.Value < currentAge {
		responseIsFresh = false
	}

	if minFresh.Valid && freshnessLifetime < currentAge+minFresh.Value {
		responseIsFresh = false
	}

	if responseIsFresh {
		return FreshnessFresh
	}

	// From https://www.rfc-editor.org/rfc/rfc9111#name-serving-stale-responses
	//
	// A "stale" response is one that either has explicit expiry information or is allowed to have heuristic expiry
	// calculated, but is not fresh according to the calculations in Section 4.2.
	if maxStale.Valid && maxStale.Value >= currentAge-freshnessLifetime {
		return FreshnessStale
	}

	return FreshnessExpired
}

// CalculateFreshnessLifetime returns how long a response can be considered to be fresh, as defined in RFC 9111,
// Section 4.2.
//
// Returns (0, false) if the freshness lifetime can not be calculated and no heuristic freshness lifetime is provided.
func CalculateFreshnessLifetime(
	privateCache bool,
	date time.Time,
	expires time.Time,
	maxAge Opt[time.Duration],
	sMaxAge Opt[time.Duration],
	heuristicFreshnessLifetime Opt[time.Duration],
) (time.Duration, bool) {
	// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-freshness-lifet
	//
	// A cache can calculate the freshness lifetime (denoted as freshness_lifetime) of a response by evaluating the
	// following rules and using the first match:

	// If the cache is shared and the s-maxage response directive (Section 5.2.2.10) is present, use its value, or
	if sMaxAge.Valid && !privateCache {
		return sMaxAge.Value, true
	}

	// If the max-age response directive (Section 5.2.2.1) is present, use its value, or
	if maxAge.Valid {
		return maxAge.Value, true
	}

	// If the Expires response header field (Section 5.3) is present, use its value minus the value of the Date
	// response header field (using the time the message was received if it is not present, as per Section 6.6.1
	// of [HTTP]), or
	if !expires.IsZero() {
		return expires.Sub(date), true
	}

	// Otherwise, no explicit expiration time is present in the response. A heuristic freshness lifetime might be
	// applicable; see Section 4.2.2.
	if heuristicFreshnessLifetime.Valid {
		return heuristicFreshnessLifetime.Value, true
	}

	return 0, false
}

// ParseAge parses a duration in seconds e.g. from the HTTP Age header or the max-/min-* Cache-Control directives.
func ParseAge(s string) (time.Duration, error) {
	d, err := parseDeltaSeconds(s)
	if err != nil {
		return 0, err
	}
	if d > uint64(math.MaxInt64/int64(time.Second)) {
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

// ParseExpires parses an Expires header value into a Go time.Time.
//
// This is the same as [http.ParseTime] except that it is case-insensitive.
func ParseExpires(s string) (time.Time, error) {
	return http.ParseTime(strings.ToUpper(s))
}

// ExtensionDirective represents a non-standard Cache-Control directive.
type ExtensionDirective struct {
	// Name of the directive. May be empty if HasValue is true.
	Name string

	// Value of the directive, if any.
	Value Opt[string]
}

// String implements the [fmt.Stringer] interface.
func (e ExtensionDirective) String() string {
	if e.Value.Valid {
		return e.Name + `="` + e.Value.Value + `"`
	}
	return e.Name
}

// ExtensionDirectives contains non-standard Cache-Control directives.
type ExtensionDirectives []ExtensionDirective

// Named returns all directives with the given name.
func (es ExtensionDirectives) Named(name string) iter.Seq[ExtensionDirective] {
	return func(yield func(ExtensionDirective) bool) {
		for _, e := range es {
			if e.Name == name && !yield(e) {
				return
			}
		}
	}
}

// Opt represents a potentially unset value.
type Opt[T any] struct {
	// Value is the value if set or the zero value of T otherwise.
	Value T

	// Valid is true if Value was set.
	Valid bool
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

	// Body contains the raw request body bytes or nil if the request did not send a body or the body was ignored.
	Body []byte

	// ParsedDirectives contains the parsed Cache-Control directives.
	//
	// This can also be obtained by parsing the Cache-Control header from the Header fields and is only set so that
	// stores can avoid parsing the header again.
	ParsedDirectives RequestDirectives
}

func requestDataFromRequest(req *http.Request, reqDirectives RequestDirectives, shouldReadBody bool) (*RequestData, error) {
	o := &RequestData{
		Time:             time.Now(),
		Method:           cmp.Or(req.Method, "GET"),
		URL:              req.URL,
		Header:           req.Header,
		ParsedDirectives: reqDirectives,
	}

	if !shouldReadBody {
		return o, nil
	}

	body, err := readBody(&req.Body, int(req.ContentLength))
	if err != nil {
		return nil, err
	}

	o.Body = body

	return o, nil
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

// RequestDirectives contains parsed cache directives from a Cache-Control header for a request.
type RequestDirectives struct {
	// https://www.rfc-editor.org/rfc/rfc9111#name-max-age
	MaxAge Opt[time.Duration]

	// https://www.rfc-editor.org/rfc/rfc9111#name-max-stale
	MaxStale Opt[time.Duration]

	// https://www.rfc-editor.org/rfc/rfc9111#name-min-fresh
	MinFresh Opt[time.Duration]

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
	Extensions ExtensionDirectives
}

var (
	errConflictingMaxAge   = errors.New("conflicting values found for directive max-age")
	errConflictingMaxStale = errors.New("conflicting values found for directive max-stale")
	errConflictingMinFresh = errors.New("conflicting values found for directive min-fresh")
	errConflictingSMaxAge  = errors.New("conflicting values found for directive s-maxage")

	errInvalidMaxAge   = errors.New("invalid value for max-age")
	errInvalidMaxStale = errors.New("invalid value for max-stale")
	errInvalidMinFresh = errors.New("invalid value for min-fresh")
	errInvalidSMaxAge  = errors.New("invalid value for s-maxage")
)

// ParseRequestDirectives parses a Cache-Control request header and returns a struct of the parsed directives.
//
// Any errors during parsing are collected and returned as one using [errors.Join] together with the struct containing
// all parseable data.
//
// Invalid or conflicting values for max-age or max-stale are considered an error, and the corresponding value will be
// set to 0, which will cause any response to be considered stale, as suggested by RFC 9111, Section 4.2.1.
//
// Similarly, an invalid or conflicting value for min-fresh will cause the value to be set to the maximum duration.
func ParseRequestDirectives(header string) (RequestDirectives, error) {
	return parseRequestDirectives(header, false)
}

// ParseStructuredRequestDirectives is like [ParseRequestDirectives], but parses a targeted Cache-Control header as
// a structured field dictionary, as required by RFC 9213.
//
// Note that RFC 9213 only specifies the Cache-Control header for the response, not for the request. We still provide
// this function for consistency.
func ParseStructuredRequestDirectives(header string) (RequestDirectives, error) {
	return parseRequestDirectives(header, true)
}

func parseRequestDirectives(header string, structured bool) (RequestDirectives, error) {
	if header == "" {
		return RequestDirectives{}, nil
	}

	var parser cachecontrol.Parser
	var err error
	if structured {
		parser, err = cachecontrol.NewParserForStructured(header)
	} else {
		parser = cachecontrol.NewParser(header)
	}
	if err != nil {
		return RequestDirectives{}, err
	}

	var c RequestDirectives
	var errs []error

	for parser.Next() {
		if err := parser.Err(); err != nil {
			errs = append(errs, err)
			continue
		}

		d := parser.Directive()

		name := strings.ToLower(d.Name)

		switch name {
		case "max-age":
			dur, err := ParseAge(d.Value)
			if err != nil {
				c.MaxAge.Value, c.MaxAge.Valid = 0, true

				errs = append(errs, errInvalidMaxAge)
				break
			}

			// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-freshness-lifet
			//
			// When there is more than one value present for a given directive (e.g., two Expires header field lines or
			// multiple Cache-Control: max-age directives), either the first occurrence should be used or the response
			// should be considered stale.
			if c.MaxAge.Valid && c.MaxAge.Value != dur {
				c.MaxAge.Value, c.MaxAge.Valid = 0, true

				errs = append(errs, errConflictingMaxAge)
				break
			}

			c.MaxAge.Value, c.MaxAge.Valid = dur, true
		case "max-stale":
			dur, err := ParseAge(d.Value)
			if err != nil {
				c.MaxStale.Value, c.MaxStale.Valid = 0, true

				errs = append(errs, errInvalidMaxStale)
				break
			}

			// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-freshness-lifet
			//
			// When there is more than one value present for a given directive (e.g., two Expires header field lines or
			// multiple Cache-Control: max-age directives), either the first occurrence should be used or the response
			// should be considered stale.
			if c.MaxStale.Valid && c.MaxStale.Value != dur {
				c.MaxStale.Value, c.MaxStale.Valid = 0, true

				errs = append(errs, errConflictingMaxStale)
				break
			}

			c.MaxStale.Value, c.MaxStale.Valid = dur, true
		case "min-fresh":
			dur, err := ParseAge(d.Value)
			if err != nil {
				c.MinFresh.Value, c.MinFresh.Valid = math.MaxInt64, true

				errs = append(errs, errInvalidMinFresh)
				break
			}

			// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-freshness-lifet
			//
			// When there is more than one value present for a given directive (e.g., two Expires header field lines or
			// multiple Cache-Control: max-age directives), either the first occurrence should be used or the response
			// should be considered stale.
			if c.MinFresh.Valid && c.MinFresh.Value != dur {
				c.MinFresh.Value, c.MinFresh.Valid = math.MaxInt64, true

				errs = append(errs, errConflictingMinFresh)
				break
			}

			c.MinFresh.Value, c.MinFresh.Valid = dur, true
		case "no-cache":
			c.NoCache = true
		case "no-store":
			c.NoStore = true
		case "no-transform":
			c.NoTransform = true
		case "only-if-cached":
			c.OnlyIfCached = true
		default:
			c.Extensions = append(c.Extensions, ExtensionDirective{
				Name:  d.Name,
				Value: Opt[string]{Value: d.Value, Valid: d.HasValue},
			})
		}
	}

	if len(errs) > 0 {
		err = errors.Join(errs...)
	}

	return c, err
}

// String implements the [fmt.Stringer] interface.
func (d RequestDirectives) String() string {
	ss := make([]string, 0, 16)
	if d.MaxAge.Valid {
		ss = append(ss, "max-age="+strconv.Itoa(int(d.MaxAge.Value/time.Second)))
	}
	if d.MaxStale.Valid {
		ss = append(ss, "max-stale="+strconv.Itoa(int(d.MaxStale.Value/time.Second)))
	}
	if d.MinFresh.Valid {
		ss = append(ss, "min-fresh="+strconv.Itoa(int(d.MinFresh.Value/time.Second)))
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

// ResponseDirectives contains parsed cache directives from a Cache-Control header for a response.
type ResponseDirectives struct {
	// https://www.rfc-editor.org/rfc/rfc9111#name-max-age-2
	MaxAge Opt[time.Duration]

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
	SMaxAge Opt[time.Duration]

	// Extensions contains all non-standard directives in the order encountered.
	//
	// The directive names are always lower cased.
	//
	// If the parsed header contained duplicate directives, this slice will also contain these duplicate directives.
	Extensions ExtensionDirectives
}

// ParseResponseDirectives parses a Cache-Control response header and returns a struct of the parsed directives.
//
// Any errors during parsing are collected and returned as one using [errors.Join] together with the struct containing
// all parseable data.
//
// Invalid or conflicting values for max-age or smax-age are considered an error, and the corresponding value will be set
// to 0, which will cause the response to be considered stale, as suggested by RFC 9111, Section 4.2.1.
func ParseResponseDirectives(header string) (ResponseDirectives, error) {
	return parseResponseDirectives(header, false)
}

// ParseStructuredResponseDirectives is like [ParseResponseDirectives], but parses a targeted Cache-Control header as
// a structured field dictionary, as required by RFC 9213.
func ParseStructuredResponseDirectives(header string) (ResponseDirectives, error) {
	return parseResponseDirectives(header, true)
}

func parseResponseDirectives(header string, structured bool) (ResponseDirectives, error) {
	if header == "" {
		return ResponseDirectives{}, nil
	}

	var parser cachecontrol.Parser
	var err error
	if structured {
		parser, err = cachecontrol.NewParserForStructured(header)
	} else {
		parser = cachecontrol.NewParser(header)
	}
	if err != nil {
		return ResponseDirectives{}, err
	}

	var c ResponseDirectives
	var errs []error

	for parser.Next() {
		if err := parser.Err(); err != nil {
			errs = append(errs, err)
			continue
		}

		d := parser.Directive()

		name := strings.ToLower(d.Name)

		switch name {
		case "max-age":
			dur, err := ParseAge(d.Value)
			if err != nil {
				c.MaxAge.Value, c.MaxAge.Valid = 0, true

				errs = append(errs, errInvalidMaxAge)
				break
			}

			// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-freshness-lifet
			//
			// When there is more than one value present for a given directive (e.g., two Expires header field lines or
			// multiple Cache-Control: max-age directives), either the first occurrence should be used or the response
			// should be considered stale.
			if c.MaxAge.Valid && c.MaxAge.Value != dur {
				c.MaxAge.Value, c.MaxAge.Valid = 0, true

				errs = append(errs, errConflictingMaxAge)
				break
			}

			c.MaxAge.Value, c.MaxAge.Valid = dur, true
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
				c.SMaxAge.Value, c.SMaxAge.Valid = 0, true

				errs = append(errs, errInvalidSMaxAge)
				break
			}

			// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-freshness-lifet
			//
			// When there is more than one value present for a given directive (e.g., two Expires header field lines or
			// multiple Cache-Control: max-age directives), either the first occurrence should be used or the response
			// should be considered stale.
			if c.SMaxAge.Valid && c.SMaxAge.Value != dur {
				c.SMaxAge.Value, c.MaxAge.Valid = 0, true

				errs = append(errs, errConflictingSMaxAge)
				break
			}

			c.SMaxAge.Value, c.SMaxAge.Valid = dur, true
		default:
			c.Extensions = append(c.Extensions, ExtensionDirective{
				Name:  d.Name,
				Value: Opt[string]{Value: d.Value, Valid: d.HasValue},
			})
		}
	}

	if len(errs) > 0 {
		err = errors.Join(errs...)
	}

	return c, err
}

// String implements the [fmt.Stringer] interface.
func (d ResponseDirectives) String() string {
	ss := make([]string, 0, 16)
	if d.MaxAge.Valid {
		ss = append(ss, "max-age="+strconv.Itoa(int(d.MaxAge.Value/time.Second)))
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
	if d.SMaxAge.Valid {
		ss = append(ss, "s-maxage="+strconv.Itoa(int(d.SMaxAge.Value/time.Second)))
	}
	for _, ext := range d.Extensions {
		ss = append(ss, ext.String())
	}
	return strings.Join(ss, ", ")
}

// Status represents a Cache-Status response header as specified in RFC 9211.
type Status struct {
	// Cache contains the name of the cache that generated the status.
	Cache string

	// Hit is true if the cache satisfied the request. Only one of Hit or Forwarded can be set.
	Hit bool

	// Forwarded contains the reason the cache forwarded the request.
	Forwarded ForwardedReason

	// ForwardedStatus, if set, contains the status code returned by the origin.
	ForwardedStatus int

	// TTL optionally contains the remaining time to live for the cached response, in seconds.
	TTL time.Duration

	// Stored is true if the response was stored in the cache.
	Stored bool

	// Collapsed indicates that the request was collapsed with other, meaning that multiple requests were combined into
	// one.
	Collapsed bool

	// Key optionally contains the cache key used for retrieving or storing the response.
	Key string

	// Detail optionally contains additional details given by the cache.
	Detail string
}

var (
	errStatusHitAndForwardedSet = errors.New("both Hit and Forwarded fields set at the same time")

	errStatusMissingCache = errors.New("missing Cache field in Status")
)

// ParseStatus parses a Cache-Status header as specified in RFC 9211.
func ParseStatus(header string) (Status, error) {
	item, err := httpsfv.Parse[httpsfv.Item](header)
	if err != nil {
		return Status{}, err
	}

	var s Status

	switch t := item.Type(); t {
	case httpsfv.BareItemTypeString:
		s.Cache = item.String()
	case httpsfv.BareItemTypeToken:
		s.Cache = item.Token()
	case httpsfv.BareItemTypeInvalid,
		httpsfv.BareItemTypeBoolean,
		httpsfv.BareItemTypeByteSequence,
		httpsfv.BareItemTypeDate,
		httpsfv.BareItemTypeDecimal,
		httpsfv.BareItemTypeDisplayString,
		httpsfv.BareItemTypeInteger:
		return Status{}, fmt.Errorf("invalid structured header item type %s", t)
	}

	if s.Cache == "" {
		return Status{}, errStatusMissingCache
	}

	if hit, ok := item.Parameters.Get("hit"); ok {
		if t := hit.Type(); t != httpsfv.BareItemTypeBoolean {
			return Status{}, fmt.Errorf("invalid hit parameter of type %s", t)
		}

		s.Hit = hit.Boolean()
	}

	if fwd, ok := item.Parameters.Get("fwd"); ok {
		if t := fwd.Type(); t != httpsfv.BareItemTypeToken {
			return Status{}, fmt.Errorf("invalid fwd parameter of type %s", t)
		}

		reason, ok := forwardedReasons[fwd.Token()]
		if !ok {
			return Status{}, fmt.Errorf("unknown fwd value %q", fwd.Token())
		}

		s.Forwarded = reason

		if s.Hit {
			return Status{}, errStatusHitAndForwardedSet
		}
	}

	if fwdStatus, ok := item.Parameters.Get("fwd-status"); ok {
		if t := fwdStatus.Type(); t != httpsfv.BareItemTypeInteger {
			return Status{}, fmt.Errorf("invalid fwd-status parameter of type %s", t)
		}

		s.ForwardedStatus = int(fwdStatus.Integer())
	}

	if ttl, ok := item.Parameters.Get("ttl"); ok {
		if t := ttl.Type(); t != httpsfv.BareItemTypeInteger {
			return Status{}, fmt.Errorf("invalid ttl parameter of type %s", t)
		}

		s.TTL = time.Duration(ttl.Integer()) * time.Second
	}

	if stored, ok := item.Parameters.Get("stored"); ok {
		if t := stored.Type(); t != httpsfv.BareItemTypeBoolean {
			return Status{}, fmt.Errorf("invalid stored parameter of type %s", t)
		}

		s.Stored = stored.Boolean()
	}

	if collapsed, ok := item.Parameters.Get("collapsed"); ok {
		if t := collapsed.Type(); t != httpsfv.BareItemTypeBoolean {
			return Status{}, fmt.Errorf("invalid collapsed parameter of type %s", t)
		}

		s.Collapsed = collapsed.Boolean()
	}

	if key, ok := item.Parameters.Get("key"); ok {
		if t := key.Type(); t != httpsfv.BareItemTypeString {
			return Status{}, fmt.Errorf("invalid key parameter of type %s", t)
		}

		s.Key = key.String()
	}

	if detail, ok := item.Parameters.Get("detail"); ok {
		t := detail.Type()

		if t != httpsfv.BareItemTypeString && t != httpsfv.BareItemTypeToken {
			return Status{}, fmt.Errorf("invalid detail parameter of type %s", t)
		}

		if t == httpsfv.BareItemTypeString {
			s.Detail = detail.String()
		} else {
			s.Detail = detail.Token()
		}
	}

	return s, nil
}

// AppendText appends the formatted status to the given slice.
//
// If [Status.Cache] is empty or both Hit and Forwarded are set, an error is returned.
//
// It implements the [encoding.TextAppender] interface.
func (s Status) AppendText(text []byte) ([]byte, error) {
	if s.Cache == "" {
		return nil, errStatusMissingCache
	}

	if s.Hit && s.Forwarded != "" {
		return nil, errStatusHitAndForwardedSet
	}

	item, _ := statusItemPool.Get().(*httpsfv.Item)
	if item == nil {
		item = &httpsfv.Item{}
	}
	defer func() {
		item.Parameters.Clear()
		statusItemPool.Put(item)
	}()

	s.intoItem(item)

	return item.AppendText(text)
}

var statusItemPool sync.Pool

func (s Status) intoItem(i *httpsfv.Item) {
	i.BareItem = httpsfv.BareItemString(s.Cache)

	if s.Hit {
		i.Parameters.Set("hit", httpsfv.BareItemBoolean(true))
	}

	if s.Forwarded != "" {
		i.Parameters.Set("fwd", httpsfv.BareItemToken(s.Forwarded.String()))
	}

	if s.ForwardedStatus != 0 {
		i.Parameters.Set("fwd-status", httpsfv.BareItemInteger(int64(s.ForwardedStatus)))
	}

	if s.TTL != 0 {
		i.Parameters.Set("ttl", httpsfv.BareItemInteger(int64(s.TTL.Seconds())))
	}

	if s.Stored {
		i.Parameters.Set("stored", httpsfv.BareItemBoolean(true))
	}

	if s.Collapsed {
		i.Parameters.Set("collapsed", httpsfv.BareItemBoolean(true))
	}

	if s.Key != "" {
		i.Parameters.Set("key", httpsfv.BareItemString(s.Key))
	}

	if s.Detail != "" {
		i.Parameters.Set("detail", httpsfv.BareItemString(s.Detail))
	}
}

// ForwardedReason is an enum of the possible reasons a request was forwarded by a cache.
type ForwardedReason string

const (
	// ForwardedReasonBypass is set when the cache was configured to not handle this request.
	ForwardedReasonBypass ForwardedReason = "bypass"

	// ForwardedReasonMethod is set when the request method's semantics require the request to be forwarded.
	ForwardedReasonMethod ForwardedReason = "method"

	// ForwardedReasonURIMiss is set when the cache did not contain any responses that matched the request URI.
	ForwardedReasonURIMiss ForwardedReason = "uri-miss"

	// ForwardedReasonVaryMiss is set when The cache contained a response that matched the request URI, but it could
	// not select a response based upon this request's header fields and stored Vary header fields.
	ForwardedReasonVaryMiss ForwardedReason = "vary-miss"

	// ForwardedReasonMiss is set when the cache did not contain any responses that could be used to satisfy this
	// request (to be used when an implementation cannot distinguish between uri-miss and vary-miss).
	ForwardedReasonMiss ForwardedReason = "miss"

	// ForwardedReasonRequest is set when the cache was able to select a fresh response for the request, but the
	// request's semantics (e.g., Cache-Control request directives) did not allow its use.
	ForwardedReasonRequest ForwardedReason = "request"

	// ForwardedReasonStale is set when the cache was able to select a response for the request, but it was stale.
	ForwardedReasonStale ForwardedReason = "stale"

	// ForwardedReasonPartial is set when the cache was able to select a partial response for the request, but it did
	// not contain all the requested ranges (or the request was for the complete response).
	ForwardedReasonPartial ForwardedReason = "partial"
)

var forwardedReasons = map[string]ForwardedReason{
	string(ForwardedReasonBypass):   ForwardedReasonBypass,
	string(ForwardedReasonMethod):   ForwardedReasonMethod,
	string(ForwardedReasonURIMiss):  ForwardedReasonURIMiss,
	string(ForwardedReasonVaryMiss): ForwardedReasonVaryMiss,
	string(ForwardedReasonMiss):     ForwardedReasonMiss,
	string(ForwardedReasonRequest):  ForwardedReasonRequest,
	string(ForwardedReasonStale):    ForwardedReasonStale,
	string(ForwardedReasonPartial):  ForwardedReasonPartial,
}

// String returns the underlying value of f.
func (f ForwardedReason) String() string {
	return string(f)
}
