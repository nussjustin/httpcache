package httpcache

import (
	"net/http"
	"slices"
	"strings"
)

// Vary contains a sorted list of unique header names, used to vary responses.
//
// Each header is canonicalized using [http.CanonicalHeaderKey].
type Vary []string

// ParseVary parses the given "Vary" response header lines.
//
// See [Vary] for more information.
func ParseVary(lines []string) Vary {
	var headers []string

	for _, line := range lines {
		for header := range strings.SplitSeq(line, ",") {
			header = strings.TrimSpace(header)
			header = http.CanonicalHeaderKey(header)

			headers = append(headers, header)
		}
	}

	if len(headers) == 0 {
		return nil
	}

	slices.Sort(headers)

	last := 0

	for i := range headers {
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

// Equals returns true if the given header maps can be considered equal based on the headers in v.
//
// This does not perform any normalization on headers, like removing spaces or joining multi-line headers.
//
// If [Vary.Wildcard] returns true, this will always return false.
func (v Vary) Equals(h1, h2 http.Header) bool {
	if v.Wildcard() {
		return false
	}

	for _, k := range v {
		k = http.CanonicalHeaderKey(k)

		v1, ok1 := h1[k]
		v2, ok2 := h2[k]

		if ok1 != ok2 || !slices.Equal(v1, v2) {
			return false
		}
	}

	return true
}

// Take returns a new header map containing only the headers from the given map that are also included in v.
//
// The slices in the returned map are copies of the originals.
//
// If v is empty or if [Vary.Wildcard] returns true, nil is returned.
func (v Vary) Take(header http.Header) http.Header {
	if len(v) == 0 || v.Wildcard() {
		return nil
	}

	m := make(http.Header, len(v))

	for _, k := range v {
		k = http.CanonicalHeaderKey(k)

		v, ok := header[k]
		if !ok {
			continue
		}

		m[k] = slices.Clone(v)
	}

	return m
}

// Wildcard returns true if v contains the wildcard value "*".
func (v Vary) Wildcard() bool {
	return slices.Contains(v, "*")
}
