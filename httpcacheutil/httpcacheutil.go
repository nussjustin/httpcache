package httpcacheutil

import (
	"hash"
	"net/url"
	"strings"
)

// HashRequest hashes the given request method, URL and, optionally body, in a way that the hash can be used as cache key.
//
// If the method is empty or HEAD, it will be treated as if it were GET.
//
// The hash contains the escaped path from the URL and no query parameters.
//
// A nil body will result in a different hash than a non-nil, but empty body.
//
// Since the query is not hashed, requests with different queries but otherwise equal method, URL and body can result
// in the same hash / cache key.
//
// Cached requests with the same hash / cache key should be matched against an incoming request based on the
// No-Vary-Search and Vary headers.
func HashRequest(h hash.Hash, method string, url_ url.URL, body []byte) (n int, err error) {
	method = strings.ToUpper(method)
	if method == "" || method == "HEAD" {
		method = "GET"
	}

	var buf [64]byte

	b := append(buf[:0], method...)
	b = append(b, 0)
	b = append(b, url_.Scheme...)
	b = append(b, 0)
	b = append(b, url_.Opaque...)
	b = append(b, 0)
	b = append(b, url_.Host...)
	b = append(b, 0)
	b = append(b, url_.EscapedPath()...)

	if body != nil {
		b = append(b, 0)
	}

	n1, err := h.Write(b)
	if err != nil {
		return n1, err
	}

	if body == nil {
		return n1, nil
	}

	n2, err := h.Write(body)
	if err != nil {
		return n1 + n2, err
	}

	return n1 + n2, nil
}
