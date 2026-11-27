package httpcache

import (
	"net/http"
	"slices"
	"strings"
	"time"
)

// Config defines characteristics of the cache based on which cacheability can be calculated.
type Config struct {
	// Name is the name of the cache for use in the Cache-Status header.
	//
	// If empty, defaults to "cache".
	Name string

	// AddCacheStatus can be set to true to set the Cache-Status header as specified in RFC 9211.
	AddCacheStatus bool

	// CacheableByExtensionFunc is an optional function that is called when checking if a response can be cached.
	//
	// If the function returns true, the response is considered cacheable.
	CacheableByExtensionFunc func(req *RequestData, resp *ResponseData) bool

	// HeuristicallyCacheableStatusCode is the list of response status codes that are considered cacheable by default.
	//
	// If nil, defaults to DefaultHeuristicallyCacheableStatusCodes.
	HeuristicallyCacheableStatusCode []int

	// HeuristicFreshnessLifetime is the default freshness lifetime used when no max-age/s-maxage Cache-Control
	// directives or Expire are set.
	HeuristicFreshnessLifetime time.Duration

	// Private configures the cache to be private, as understood by RFC 9111.
	Private bool

	// RespectRequestDirectiveNoCache determines whether [Client.Do] checks no-cache Cache-Control directive of the request.
	//
	// If true, the directive is checked and, if set, causes the method to return false. Otherwise, the directive is
	// ignored.
	RespectRequestDirectiveNoCache bool

	// RespectRequestDirectiveNoStore can be set to enable checking of the no-store Cache-Control request directive
	// in [Config.AllowsStoringResponse].
	//
	// Note that while RFC 9111 specifies that the no-store directive should prevent responses from being cached, the
	// steps for determining whether a response can be stored do not actually say anything about the directive.
	RespectRequestDirectiveNoStore bool

	// RespectResponseDirectivePrivateValue can be set to true to allow storing responses even when the private
	// directive is specified, as long as the private directive has specified at least one header in its value.
	//
	// It also causes [Config.RemoveUnstorableHeaders] to remove headers specified for the "private" response directive,
	// but not for the "no-cache" directive (as those are still usable depending on the request).
	//
	// If false, the directive is treated as if it had no value.
	RespectResponseDirectivePrivateValue bool

	// StoreProxyHeaders, if set, causes [Config.RemoveUnstorableHeaders] to not remove the following headers:
	//
	// - Proxy-Authenticate
	// - Proxy-Authentication-Info
	// - Proxy-Authorization
	StoreProxyHeaders bool

	// SupportedRequestMethods is the list of request methods that can be cached.
	//
	// If nil, defaults to [DefaultSupportedRequestMethods].
	SupportedRequestMethods []string

	// UnderstoodResponseCodes must contain the response codes that the cache understands.
	//
	// Responses with status codes 206 or 304 or the must-understand directive must be understood to be cacheable.
	//
	// Other responses are not required to be understood.
	UnderstoodResponseCodes []int

	// IsSafeMethodFunc is an optional function that when set is used by [Config.IsSafeMethod] to check if a method
	// is safe.
	//
	// The method is guaranteed to be in all uppercase.
	IsSafeMethodFunc func(method string) bool

	// TargetList, if not empty, contains alternative headers that a [Client] will check in favor of Cache-Control, as
	// specified in RFC 9213.
	TargetList []string

	// RequestMethodsWithoutBody optionally defines the set of request methods for which the body should be ignored,
	// when looking up or caching a response.
	//
	// If nil, [DefaultRequestMethodsWithoutBody] is used.
	RequestMethodsWithoutBody []string
}

// DefaultSupportedRequestMethods is the default list of request methods that allow caching.
var DefaultSupportedRequestMethods = []string{
	"GET",
	"HEAD",
	"QUERY",
}

// DefaultHeuristicallyCacheableStatusCodes contains HTTP response codes specified in RFC 9110 that are allowed to be
// cached by default, that is without requiring extra headers.
var DefaultHeuristicallyCacheableStatusCodes = []int{
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

// DefaultRequestMethodsWithoutBody is the default list of request methods for which the body is ignored when looking
// up or storing responses.
var DefaultRequestMethodsWithoutBody = []string{
	"GET",
	"HEAD",
}

// AllowsStoringResponse checks if the given response data can be cached.
//
// Note that the response data may not contain a body.
func (c Config) AllowsStoringResponse(req *RequestData, resp *ResponseData) bool {
	// 3. Storing Responses in Caches
	//
	// A cache MUST NOT store a response to a request unless:

	// - the request method is understood by the cache;
	if !c.isSupportedRequestMethod(req.Method) {
		return false
	}

	// - the response status code is final (see Section 15 of [HTTP]);
	if resp.StatusCode < 200 {
		return false
	}

	// - if the response status code is 206 or 304, or the must-understand cache directive (see Section 5.2.2.3) is
	//   present: the cache understands the response status code
	if resp.StatusCode == http.StatusPartialContent || resp.StatusCode == http.StatusNotModified || resp.ParsedDirectives.MustUnderstand {
		if !c.isUnderstoodResponseCode(resp.StatusCode) {
			return false
		}
	}

	// - the no-store cache directive is not present in the response (see Section 5.2.2.5);
	if resp.ParsedDirectives.NoStore {
		return false
	}

	// - if the cache is shared: the private response directive is either not present or allows a shared cache to store
	//   a modified response; see Section 5.2.2.7);
	if !c.Private && resp.ParsedDirectives.Private && (!c.RespectResponseDirectivePrivateValue || len(resp.ParsedDirectives.PrivateHeaders) == 0) {
		return false
	}

	// - if the cache is shared: the Authorization header field is not present in the request (see Section 11.6.2 of
	//   [HTTP]) or a response directive is present that explicitly allows shared caching (see Section 3.5); and
	if !c.Private && len(req.Header["Authorization"]) != 0 && !resp.ParsedDirectives.MustRevalidate && !resp.ParsedDirectives.Public && !resp.ParsedDirectives.SMaxAge.Valid {
		return false
	}

	// - the response contains at least one of the following
	switch {
	// a public response directive (see Section 5.2.2.9);
	case resp.ParsedDirectives.Public:
	// a private response directive, if the cache is not shared (see Section 5.2.2.7);
	case c.Private && resp.ParsedDirectives.Private:
	// an Expires header field (see Section 5.3);
	case hasValidExpires(resp.Header):
	// a max-age response directive (see Section 5.2.2.1);
	case resp.ParsedDirectives.MaxAge.Valid:
	// if the cache is shared: an s-maxage response directive (see Section 5.2.2.10);
	case !c.Private && resp.ParsedDirectives.SMaxAge.Valid:
	// a cache extension that allows it to be cached (see Section 5.2.3); or
	case c.CacheableByExtensionFunc != nil && c.CacheableByExtensionFunc(req, resp):
	// a status code that is defined as heuristically cacheable (see Section 4.2.2).
	case c.isHeuristicallyCacheableStatusCode(resp.StatusCode):
	default:
		return false
	}

	if c.RespectRequestDirectiveNoStore {
		// Note: This is not actually part of "3. Storing Responses in Caches".
		if req.ParsedDirectives.NoStore {
			return false
		}
	}

	return true
}

// ParseRequestDirectives parses the Cache-Control directives for a request by joining all Cache-Control headers with a
// comma and passing the result to [ParseRequestDirectives].
func (c Config) ParseRequestDirectives(h http.Header) (RequestDirectives, error) {
	return ParseRequestDirectives(strings.Join(h["Cache-Control"], ","))
}

// ParseResponseDirectives parses the Cache-Control directives for a response, respecting the targeted cache control
// headers defined in [Config.TargetList], as specified in RFC 9213.
func (c Config) ParseResponseDirectives(h http.Header) (ResponseDirectives, error) {
	for _, name := range c.TargetList {
		s := h[name]
		if len(s) == 0 {
			continue
		}

		d, err := ParseStructuredResponseDirectives(strings.Join(s, ","))
		if err != nil {
			continue
		}
		return d, nil
	}

	return ParseResponseDirectives(strings.Join(h["Cache-Control"], ","))
}

func hasValidExpires(h http.Header) bool {
	ss := h["Expires"]

	if len(ss) == 0 {
		return false
	}

	// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-freshness-lifet
	//
	// When there is more than one value present for a given directive (e.g., two Expires header field lines or
	// multiple Cache-Control: max-age directives), either the first occurrence should be used or the response
	// should be considered stale.
	expires, _ := ParseExpires(ss[0])
	return !expires.IsZero()
}

func (c Config) isHeuristicallyCacheableStatusCode(code int) bool {
	s := c.HeuristicallyCacheableStatusCode
	if s == nil {
		s = DefaultHeuristicallyCacheableStatusCodes
	}
	return slices.Contains(s, code)
}

func (c Config) isSupportedRequestMethod(method string) bool {
	s := c.SupportedRequestMethods
	if s == nil {
		s = DefaultSupportedRequestMethods
	}
	return slices.Contains(s, method)
}

func (c Config) isUnderstoodResponseCode(code int) bool {
	return slices.Contains(c.UnderstoodResponseCodes, code)
}

// IsSafeMethod returns true if the given method is considered safe.
//
// By default, only GET, HEAD, OPTIONS, QUERY and TRACE are considered unsafe.
//
// If [Config.IsSafeMethodFunc] is not nil, it will be called instead and the result returned.
//
// The method will be normalized to uppercase in any case.
func (c Config) IsSafeMethod(method string) bool {
	method = strings.ToUpper(method)

	if c.IsSafeMethodFunc != nil {
		return c.IsSafeMethodFunc(method)
	}

	switch method {
	case "GET", "HEAD", "OPTIONS", "QUERY", "TRACE":
		return true
	default:
		return false
	}
}

// RemoveUnstorableHeaders removes response headers that must not be stored.
//
// It returns the updated map.
//
// If copyOnChange is true, the map will be copied before removing headers and the copy will be returned.
func (c Config) RemoveUnstorableHeaders(headers http.Header, copyOnChange bool) http.Header {
	var copied bool

	del := func(name string) {
		if _, ok := headers[name]; !ok {
			return
		}

		if copyOnChange && !copied {
			headers = headers.Clone()
			copied = true
		}

		delete(headers, name)
	}

	//  3.1. Storing Header and Trailer Fields
	//
	// Caches MUST include all received response header fields -- including unrecognized ones -- when storing a response;
	// this assures that new HTTP header fields can be successfully deployed. However, the following exceptions are made:

	connection := headers["Connection"]

	// The Connection header field and fields whose names are listed in it are required by Section 7.6.1 of [HTTP] to be
	// removed before forwarding the message. This MAY be implemented by doing so before storage.
	del("Connection")

	// Likewise, some fields' semantics require them to be removed before forwarding the message, and this MAY be
	// implemented by doing so before storage; see Section 7.6.1 of [HTTP] for some examples.
	for _, tokens := range connection {
		for header := range strings.FieldsSeq(tokens) {
			del(http.CanonicalHeaderKey(header))
		}
	}

	if c.RespectResponseDirectivePrivateValue {
		// The no-cache (Section 5.2.2.4) and private (Section 5.2.2.7) cache directives can have arguments that prevent
		// storage of header fields by all caches and shared caches, respectively.

		directives, _ := c.ParseResponseDirectives(headers)

		for _, header := range directives.PrivateHeaders {
			del(http.CanonicalHeaderKey(header))
		}
	}

	if !c.StoreProxyHeaders {
		// Header fields that are specific to the proxy that a cache uses when forwarding a request MUST NOT be stored,
		// unless the cache incorporates the identity of the proxy into the cache key. Effectively, this is limited to
		// Proxy-Authenticate (Section 11.7.1 of [HTTP]), Proxy-Authentication-Info (Section 11.7.3 of [HTTP]), and
		// Proxy-Authorization (Section 11.7.2 of [HTTP]).
		del("Proxy-Authenticate")
		del("Proxy-Authentication-Info")
		del("Proxy-Authorization")
	}

	return headers
}

func (c Config) shouldReadBodyForMethod(method string) bool {
	ignored := c.RequestMethodsWithoutBody
	if ignored == nil {
		ignored = DefaultRequestMethodsWithoutBody
	}
	return !slices.Contains(ignored, method)
}
