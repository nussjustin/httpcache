package httpcacheutil

import (
	"fmt"
	"hash"
	"net/url"
	"strconv"
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
	b = append(b, url_.Hostname()...)
	b = append(b, 0)
	if p := url_.Port(); p != "" {
		b = append(b, url_.Port()...)
	} else {
		b = strconv.AppendUint(b, uint64(defaultPort[strings.ToLower(url_.Scheme)]), 10)
	}
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

// Origin represents an Origin as defined in RFC 9110, Section 4.3.1 URI Origin.
type Origin struct {
	_ [0]func()

	// Scheme contains the lower-cased scheme, e.g. "https".
	Scheme string

	// Host contains the host name without any normalization.
	Host string

	// Port contains the port.
	Port uint16
}

var defaultPort = map[string]uint16{
	"ftp":   21,
	"http":  80,
	"https": 443,
	"sftp":  22,
	"ssh":   22,
}

// OriginFromURL returns the origin for the given URL.
//
// The scheme and port will be normalized as required by RFC 9110 and a missing port will use the default for the
// scheme, if known.
func OriginFromURL(u url.URL) (Origin, error) {
	scheme := strings.ToLower(u.Scheme)

	port := defaultPort[scheme]
	if portStr := u.Port(); portStr != "" {
		parsed, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return Origin{}, fmt.Errorf("failed to parse port: %w", err)
		}
		port = uint16(parsed)
	}

	return Origin{
		Scheme: scheme,
		Host:   u.Hostname(),
		Port:   port,
	}, nil
}

// MustOriginFromURL is like [OriginFromURL], but panics on error.
func MustOriginFromURL(u url.URL) Origin {
	o, err := OriginFromURL(u)
	if err != nil {
		panic(fmt.Errorf("failed to get origin from url.URL: %w", err))
	}
	return o
}

// Same returns true if other is considered the same origin as o.
func (o Origin) Same(other Origin) bool {
	return o.Scheme == other.Scheme && o.Host == other.Host && o.Port == other.Port
}
