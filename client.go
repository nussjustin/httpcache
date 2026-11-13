package httpcache

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"
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
		Header:        resp.Header.Clone(),
		Body:          io.NopCloser(bytes.NewBuffer(body)),
		ContentLength: int64(len(body)),
		Trailer:       resp.Trailer.Clone(),
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

func (c *Client) do(req *http.Request) (*http.Response, Status, error) {
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	var status Status
	if c.Config.Name != "" {
		status.Cache = c.Config.Name
	} else {
		status.Cache = "cache"
	}

	if !c.Config.isSupportedRequestMethod(req.Method) {
		resp, err := client.Do(req)
		status.Forwarded = ForwardedReasonMethod
		return resp, status, err
	}

	if len(req.Header["Expect"]) != 0 {
		resp, err := client.Do(req)
		status.Forwarded = ForwardedReasonRequest
		return resp, status, err
	}

	var reqDirectives RequestDirectives
	if s := strings.Join(req.Header["Cache-Control"], ","); s != "" {
		reqDirectives, _ = ParseRequestDirectives(s)
	}

	if c.Config.RespectRequestDirectiveNoCache && reqDirectives.NoCache {
		resp, err := client.Do(req)
		status.Forwarded = ForwardedReasonRequest
		return resp, status, err
	}

	stored, err := c.Store.Get(req.Context(), req)

	if stored != nil && err == nil {
		age, _ := ParseAge(stored.Header.Get("Age"))

		date, _ := http.ParseTime(stored.Header.Get("Date"))

		// From https://www.rfc-editor.org/rfc/rfc9111#name-calculating-freshness-lifet
		//
		// When there is more than one value present for a given directive (e.g., two Expires header field lines or
		// multiple Cache-Control: max-age directives), either the first occurrence should be used or the response
		// should be considered stale.
		expires, _ := ParseExpires(stored.Header.Get("Expires"))

		respDirectives, _ := c.Config.ParseResponseDirectives(stored.Header)

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

		status.TTL = freshnessLifetime - age

		switch freshness {
		case FreshnessExpired:
			status.Forwarded = ForwardedReasonMiss
			stored = nil
		case FreshnessFresh:
			status.Hit = true
			return stored, status, nil
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

	reqTime := time.Now()

	//goland:noinspection GoResourceLeak
	resp, err := client.Do(req)
	if err != nil {
		return nil, Status{}, err
	}

	respTime := time.Now()

	if stored != nil && resp.StatusCode == http.StatusNotModified {
		return stored, status, nil
	}

	if c.Config.AllowsStoringResponse(resp) {
		respCopy, err := cloneResponse(resp)
		if err != nil {
			return nil, Status{}, err
		}

		c.Config.RemoveUnstorableHeaders(respCopy.Header)

		if err := c.Store.Set(req.Context(), req, reqTime, respCopy, respTime); err == nil {
			status.Stored = true
		}
	}

	return resp, status, nil
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
	// The given request must not be modified.
	//
	// The response must include an Age header containing the age of the response.
	Get(ctx context.Context, req *http.Request) (resp *http.Response, err error)

	// Set stores the given response in the cache.
	//
	// The given request must not be modified or retained.
	//
	// The response is a copy of the original, without the Request field set, and the body is guaranteed to be
	// readable without errors.
	Set(
		ctx context.Context,
		req *http.Request, reqTime time.Time,
		resp *http.Response, respTime time.Time,
	) error
}
