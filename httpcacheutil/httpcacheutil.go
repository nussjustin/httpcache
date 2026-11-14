package httpcacheutil

import (
	"bytes"
	"hash"
	"io"
	"net/http"
	"strings"
)

// BodyBuffer wraps a bytes.Buffer so that it implements the [io.Closer] interface for use in the
// [net/http.Request.Body] and [net/http.Response.Body] fields.
type BodyBuffer struct {
	bytes.Buffer
}

// Close implements the [io.Closer] interface.
func (b *BodyBuffer) Close() error {
	return nil
}

// CloneResponse clones the given response including its body.
//
// The response will have the following fields populated:
// - Status
// - StatusCode
// - Proto
// - ProtoMajor
// - ProtoMinor
// - Header
// - Trailer
// - ContentLength
// - Body, if ContentLength is > 0
//
// The body is copied into a [*BodyBuffer] using [CopyResponseBody].
func CloneResponse(resp *http.Response) (*http.Response, error) {
	respClone := &http.Response{
		Status:     resp.Status,
		StatusCode: resp.StatusCode,
		Proto:      resp.Proto,
		ProtoMajor: resp.ProtoMajor,
		ProtoMinor: resp.ProtoMinor,
		Header:     resp.Header.Clone(),
		Trailer:    resp.Trailer.Clone(),
	}

	var body BodyBuffer
	var err error

	if respClone.ContentLength, err = CopyResponseBody(&body, resp); err != nil {
		return nil, err
	}

	if respClone.ContentLength > 0 {
		respClone.Body = &body
	}

	return respClone, nil
}

// CopyRequestBody copies the body from resp to dst while making sure the request body can still be read afterward.
//
// It works like [CopyResponseBody] with the exception that if the [net/http.Request.GetBody] field is set,
// CopyRequestBody will call that to create a new reader that is than copied to dst, and ignore the Body field.
func CopyRequestBody(dst io.Writer, req *http.Request) (n int64, err error) {
	if req.GetBody != nil {
		rc, err := req.GetBody()
		if err != nil {
			return 0, err
		}
		defer func() {
			if cerr := rc.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()

		return io.Copy(dst, rc)
	}

	return copyReadCloser(dst, &req.Body, req.ContentLength)
}

// CopyResponseBody copies the body from resp to dst while making sure the response body can still be read afterward.
//
// If the response body is a [*bytes.Buffer] or [*BodyBuffer], the bytes will be written to dst without modifying the
// buffer.
//
// If the response body implements the [io.Seeker] interface, the body will be read and rewound by the number of
// bytes reader.
//
// Otherwise, the body will be read in its entirety and the Body will replaced with a [*BodyBuffer] containing the
// original body.
//
// If the response body is nil, nothing will be copied.
func CopyResponseBody(dst io.Writer, resp *http.Response) (n int64, err error) {
	return copyReadCloser(dst, &resp.Body, resp.ContentLength)
}

func copyReadCloser(dst io.Writer, src *io.ReadCloser, srcLen int64) (n int64, err error) {
	r := io.Reader(*src)

	if r == nil {
		return 0, nil
	}

	switch v := r.(type) {
	case *BodyBuffer:
		n1, err := dst.Write(v.Bytes())
		return int64(n1), err
	case io.Seeker:
		defer func() {
			if _, serr := v.Seek(-n, io.SeekCurrent); serr != nil && err == nil {
				err = serr
			}
		}()

		return io.Copy(dst, r)
	}

	var buf BodyBuffer

	// Pre-allocate space in buffer if we have an idea of the length.
	if srcLen > 0 {
		const maxPreAlloc = 4 << 20 // 4MiB

		buf.Grow(int(min(srcLen, maxPreAlloc)))
	}

	n, err = io.Copy(io.MultiWriter(dst, &buf), r)
	if err != nil {
		return n, err
	}

	*src = &buf

	return n, nil
}

// HashRequest writes the given request to h, which can then be used to generate a key for caching.
//
// The hash will contain the method and URL, with escaped path and without query.
//
// If the method is empty or HEAD, it will be treated as if it were GET.
//
// If body is true, the request body will be written to h using [CopyRequestBody].
//
// Since the query is not hashed, requests with different queries but otherwise equal method, URL and body can result
// in the same hash / cache key.
//
// Cached requests with the same hash / cache key should be matched against an incoming request based on the
// No-Vary-Search and Vary headers.
func HashRequest(h hash.Hash, req *http.Request, body bool) (n int, err error) {
	method := strings.ToUpper(req.Method)
	if method == "" || method == "HEAD" {
		method = "GET"
	}

	var buf [64]byte

	b := append(buf[:0], method...)
	b = append(b, 0)
	b = append(b, req.URL.Scheme...)
	b = append(b, 0)
	b = append(b, req.URL.Opaque...)
	b = append(b, 0)
	b = append(b, req.URL.Host...)
	b = append(b, 0)
	b = append(b, req.URL.EscapedPath()...)

	if body {
		b = append(b, 0)
	}

	n1, err := h.Write(b)
	if err != nil {
		return n1, err
	}

	if !body {
		return n1, nil
	}

	n2, err := CopyRequestBody(h, req)
	if err != nil {
		return n1 + int(n2), err
	}

	return n1 + int(n2), nil
}
