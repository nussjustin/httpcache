package httpcache

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

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

	reqData, err := requestDataFromRequest(req, reqDirectives, c.Config.shouldReadBodyForMethod(req.Method))
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
