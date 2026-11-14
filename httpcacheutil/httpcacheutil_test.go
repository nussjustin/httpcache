package httpcacheutil_test

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/nussjustin/httpcache/httpcacheutil"
)

type dummyHash struct {
	b []byte
}

func (d *dummyHash) BlockSize() int {
	return 1
}

func (d *dummyHash) Reset() {
	d.b = nil
}

func (d *dummyHash) Size() int {
	return len(d.b)
}

func (d *dummyHash) Sum(b []byte) []byte {
	return append(b, d.b...)
}

func (d *dummyHash) Write(p []byte) (n int, err error) {
	d.b = append(d.b, p...)
	return len(p), nil
}

type dummyNopCloser struct {
	io.Reader
}

func (d dummyNopCloser) Close() error {
	return nil
}

type errReader struct {
	err error
}

func (e errReader) Read([]byte) (n int, err error) {
	return 0, e.err
}

type testReadSeekCloser struct {
	s       string
	off     int
	closed  bool
	err     error
	seekErr error
}

func (t *testReadSeekCloser) Close() error {
	if t.err != nil {
		return t.err
	}
	t.closed = true
	t.err = errors.New("closed")
	return nil
}

func (t *testReadSeekCloser) Read(p []byte) (n int, err error) {
	if t.err != nil {
		return 0, t.err
	}
	if t.off >= len(t.s) {
		return 0, io.EOF
	}
	n = copy(p, t.s[t.off:])
	t.off += n
	return n, nil
}

func (t *testReadSeekCloser) Seek(offset int64, whence int) (int64, error) {
	if t.err != nil {
		return 0, t.err
	}
	if t.seekErr != nil {
		return 0, t.seekErr
	}
	var newOff int
	switch whence {
	case io.SeekStart:
		newOff = int(offset)
	case io.SeekCurrent:
		newOff = t.off + int(offset)
	case io.SeekEnd:
		newOff = len(t.s) - whence
	}
	if newOff < 0 {
		return 0, errors.New("seek out of bounds")
	}
	t.off = newOff
	return int64(newOff), nil
}

func TestCloneResponse(t *testing.T) {
	orig := http.Response{
		Status:     "Somewhat OK",
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.2",
		ProtoMajor: 1,
		ProtoMinor: 2,
		Header: http.Header{
			"Accept":       []string{"application/json"},
			"Content-Type": []string{"application/json"},
		},
		Body:             dummyNopCloser{strings.NewReader("hello world")},
		ContentLength:    -1,
		TransferEncoding: []string{"identity"},
		Close:            true,
		Uncompressed:     true,
		Trailer: http.Header{
			"Server-Timing": []string{"db;dur=1337"},
		},
		Request: &http.Request{Method: "GET"},
		TLS:     &tls.ConnectionState{},
	}

	resp := new(orig)
	resp.Header = orig.Header.Clone()
	resp.Trailer = orig.Trailer.Clone()

	got, err := httpcacheutil.CloneResponse(resp) //nolint:bodyclose
	if err != nil {
		t.Fatalf("CloseResponse() err = %v, want nil", err)
	}

	if got == resp {
		t.Fatalf("CloseResponse() returned original response")
	}

	gotBody := got.Body
	got.Body = nil

	want := &http.Response{
		Status:     "Somewhat OK",
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.2",
		ProtoMajor: 1,
		ProtoMinor: 2,
		Header: http.Header{
			"Accept":       []string{"application/json"},
			"Content-Type": []string{"application/json"},
		},
		ContentLength: int64(len("hello world")),
		Trailer: http.Header{
			"Server-Timing": []string{"db;dur=1337"},
		},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("CloneResponse() mismatch (-want +git):\n%s", diff)
	}

	if reflect.ValueOf(got.Header).UnsafePointer() == reflect.ValueOf(resp.Header).UnsafePointer() {
		t.Errorf("CloneResponse() did not clone Header")
	}

	for k := range got.Header {
		gotSlice, respSlice := got.Header[k], resp.Header[k]
		if &gotSlice[0] == &respSlice[0] {
			t.Errorf("CloneResponse() did not clone Header values")
		}
	}

	if reflect.ValueOf(got.Trailer).UnsafePointer() == reflect.ValueOf(resp.Trailer).UnsafePointer() {
		t.Errorf("CloneResponse() did not clone Trailer")
	}

	for k := range got.Trailer {
		gotSlice, respSlice := got.Trailer[k], resp.Trailer[k]
		if &gotSlice[0] == &respSlice[0] {
			t.Errorf("CloneResponse() did not clone Header values")
		}
	}

	assertReader(t, "CloneResponse().Body", "hello world", gotBody)

	assertReader(t, "original response body after CloneResponse()", "hello world", resp.Body)
}

func TestCloneResponse_ReadError(t *testing.T) {
	resp := &http.Response{
		Body: dummyNopCloser{errReader{io.ErrClosedPipe}},
	}

	respClone, err := httpcacheutil.CloneResponse(resp) //nolint:bodyclose
	if err == nil {
		t.Errorf("CloseResponse() err = nil, want %v", io.ErrClosedPipe)
	}
	if respClone != nil {
		t.Errorf("CloneResponse() = %v, want nil", respClone)
	}
}

func TestCopyRequestBody(t *testing.T) {
	t.Run("no body", func(t *testing.T) {
		const want = ""

		var dst bytes.Buffer

		resp := &http.Request{}

		n, err := httpcacheutil.CopyRequestBody(&dst, resp)
		assertCopy(t, "CopyRequestBody", &dst, n, err, want, nil)

		if resp.Body != nil {
			t.Errorf("Response.Body = %v after CopyRequestBody(), want nil", resp.Body)
		}
	})

	t.Run("generic", func(t *testing.T) {
		const want = "hello world"

		var dst bytes.Buffer

		resp := &http.Request{
			Body: dummyNopCloser{strings.NewReader(want)},
		}

		n, err := httpcacheutil.CopyRequestBody(&dst, resp)
		assertCopy(t, "CopyRequestBody", &dst, n, err, want, nil)

		assertReader(t, "Response.Body after CopyRequestBody()", want, resp.Body)
	})

	t.Run("generic reader error", func(t *testing.T) {
		const want = ""

		var dst bytes.Buffer

		resp := &http.Request{
			Body: dummyNopCloser{errReader{io.ErrClosedPipe}},
		}

		n, err := httpcacheutil.CopyRequestBody(&dst, resp)
		assertCopy(t, "CopyRequestBody", &dst, n, err, want, io.ErrClosedPipe)
	})

	t.Run("with BodyBuffer", func(t *testing.T) {
		const want = "hello BodyBuffer"

		var dst bytes.Buffer

		b := &httpcacheutil.BodyBuffer{*bytes.NewBufferString(want)}
		wantPtr := &b.Bytes()[0]

		resp := &http.Request{Body: b}

		n, err := httpcacheutil.CopyRequestBody(&dst, resp)
		assertCopy(t, "CopyRequestBody", &dst, n, err, want, nil)

		if resp.Body != b {
			t.Error("CopyRequestBody() changed .Body")
		} else if gotPtr := &b.Bytes()[0]; gotPtr != wantPtr {
			t.Errorf("CopyResonseBody() changed BodyBuffer slice")
		}
	})

	t.Run("with io.Seeker", func(t *testing.T) {
		const want = "hello io.Seeker"

		var dst bytes.Buffer

		b := &testReadSeekCloser{s: "prefix" + want}

		offset, err := b.Seek(int64(len("prefix")), io.SeekStart)
		if err != nil {
			t.Fatalf("failed to seek past prefix: %s", err)
		}

		resp := &http.Request{Body: b}

		n, err := httpcacheutil.CopyRequestBody(&dst, resp)
		assertCopy(t, "CopyRequestBody", &dst, n, err, want, nil)

		if resp.Body != b {
			t.Error("CopyRequestBody() changed .Body")
		} else if b.off != int(offset) {
			t.Errorf("CopyResonseBody() did not reset offset. Got %d, want %d", b.off, offset)
		}
	})

	t.Run("with io.Seeker error when seeking", func(t *testing.T) {
		const want = "hello io.Seeker"

		var dst bytes.Buffer

		b := &testReadSeekCloser{s: want, seekErr: io.ErrNoProgress}

		resp := &http.Request{Body: b}

		n, err := httpcacheutil.CopyRequestBody(&dst, resp)
		assertCopy(t, "CopyRequestBody", &dst, n, err, want, io.ErrNoProgress)
	})

	t.Run("GetBody", func(t *testing.T) {
		const want = "hello GetBody"

		var dst bytes.Buffer

		resp := &http.Request{
			GetBody: func() (io.ReadCloser, error) {
				return dummyNopCloser{Reader: strings.NewReader(want)}, nil
			},
			Body: dummyNopCloser{Reader: strings.NewReader("unreachable")},
		}

		n, err := httpcacheutil.CopyRequestBody(&dst, resp)
		assertCopy(t, "CopyRequestBody", &dst, n, err, want, nil)
	})

	t.Run("GetBody error", func(t *testing.T) {
		const want = ""

		var dst bytes.Buffer

		resp := &http.Request{
			GetBody: func() (io.ReadCloser, error) {
				return nil, io.ErrClosedPipe
			},
			Body: dummyNopCloser{Reader: strings.NewReader("unreachable")},
		}

		n, err := httpcacheutil.CopyRequestBody(&dst, resp)
		assertCopy(t, "CopyRequestBody", &dst, n, err, want, io.ErrClosedPipe)
	})
}

func TestCopyResponseBody(t *testing.T) {
	t.Run("no body", func(t *testing.T) {
		const want = ""

		var dst bytes.Buffer

		resp := &http.Response{}

		n, err := httpcacheutil.CopyResponseBody(&dst, resp)
		assertCopy(t, "CopyResponseBody", &dst, n, err, want, nil)

		if resp.Body != nil {
			t.Errorf("Response.Body = %v after CopyResponseBody(), want nil", resp.Body)
		}
	})

	t.Run("generic", func(t *testing.T) {
		const want = "hello world"

		var dst bytes.Buffer

		resp := &http.Response{
			Body: dummyNopCloser{strings.NewReader(want)},
		}

		n, err := httpcacheutil.CopyResponseBody(&dst, resp)
		assertCopy(t, "CopyResponseBody", &dst, n, err, want, nil)

		assertReader(t, "Response.Body after CopyResponseBody()", want, resp.Body)
	})

	t.Run("generic reader error", func(t *testing.T) {
		const want = ""

		var dst bytes.Buffer

		resp := &http.Response{
			Body: dummyNopCloser{errReader{io.ErrClosedPipe}},
		}

		n, err := httpcacheutil.CopyResponseBody(&dst, resp)
		assertCopy(t, "CopyResponseBody", &dst, n, err, want, io.ErrClosedPipe)
	})

	t.Run("with BodyBuffer", func(t *testing.T) {
		const want = "hello BodyBuffer"

		var dst bytes.Buffer

		b := &httpcacheutil.BodyBuffer{*bytes.NewBufferString(want)}
		wantPtr := &b.Bytes()[0]

		resp := &http.Response{Body: b}

		n, err := httpcacheutil.CopyResponseBody(&dst, resp)
		assertCopy(t, "CopyResponseBody", &dst, n, err, want, nil)

		if resp.Body != b {
			t.Error("CopyResponseBody() changed .Body")
		} else if gotPtr := &b.Bytes()[0]; gotPtr != wantPtr {
			t.Errorf("CopyResonseBody() changed BodyBuffer slice")
		}
	})

	t.Run("with io.Seeker", func(t *testing.T) {
		const want = "hello io.Seeker"

		var dst bytes.Buffer

		b := &testReadSeekCloser{s: "prefix" + want}

		offset, err := b.Seek(int64(len("prefix")), io.SeekStart)
		if err != nil {
			t.Fatalf("failed to seek past prefix: %s", err)
		}

		resp := &http.Response{Body: b}

		n, err := httpcacheutil.CopyResponseBody(&dst, resp)
		assertCopy(t, "CopyResponseBody", &dst, n, err, want, nil)

		if resp.Body != b {
			t.Error("CopyResponseBody() changed .Body")
		} else if b.off != int(offset) {
			t.Errorf("CopyResonseBody() did not reset offset. Got %d, want %d", b.off, offset)
		}
	})

	t.Run("with io.Seeker error when seeking", func(t *testing.T) {
		const want = "hello io.Seeker"

		var dst bytes.Buffer

		b := &testReadSeekCloser{s: want, seekErr: io.ErrNoProgress}

		resp := &http.Response{Body: b}

		n, err := httpcacheutil.CopyResponseBody(&dst, resp)
		assertCopy(t, "CopyResponseBody", &dst, n, err, want, io.ErrNoProgress)
	})
}

func assertCopy(tb testing.TB, fn string, dst *bytes.Buffer, n int64, err error, want string, wantErr error) {
	tb.Helper()
	if !errors.Is(err, wantErr) {
		tb.Errorf("%s() err = %v, want %v", fn, err, wantErr)
	}
	if int(n) != len(want) {
		tb.Errorf("%s() = %d, want %d", fn, n, len(want))
	}
	if dst.String() != want {
		tb.Errorf("%s() wrote %q, want %q", fn, dst.String(), want)
	}
}

func TestHashRequest(t *testing.T) {
	hash := func(tb testing.TB, req *http.Request, body bool) (string, error) {
		var h dummyHash

		n, err := httpcacheutil.HashRequest(&h, req, body)
		if err != nil {
			return "", err
		}

		if n != len(h.b) {
			tb.Errorf("HashRequest() n = %d, want %d", n, len(h.b))
		}

		return string(h.b), nil
	}

	tests := []struct {
		name         string
		reqMethod    string
		reqURL       string
		reqBody      *string
		reqBodyError error
		body         bool
		want         string
		wantErr      bool
	}{
		{
			name:      "GET",
			reqMethod: "GET",
			reqURL:    "https://example.com/",
			want:      "GET\x00https\x00\x00example.com\x00/",
		},
		{
			name:      "HEAD",
			reqMethod: "HEAD",
			reqURL:    "https://example.com/",
			want:      "GET\x00https\x00\x00example.com\x00/",
		},
		{
			name:      "POST with body",
			reqMethod: "POST",
			reqURL:    "https://example.com/",
			reqBody:   new("hello world"),
			body:      true,
			want:      "POST\x00https\x00\x00example.com\x00/\x00hello world",
		},
		{
			name:      "custom method without body",
			reqMethod: "CUSTOM",
			reqURL:    "https://example.com/",
			want:      "CUSTOM\x00https\x00\x00example.com\x00/",
		},
		{
			name:      "custom method with empty body",
			reqMethod: "CUSTOM",
			reqURL:    "https://example.com/",
			reqBody:   new(""),
			body:      true,
			want:      "CUSTOM\x00https\x00\x00example.com\x00/\x00",
		},
		{
			name:      "custom method with body",
			reqMethod: "CUSTOM",
			reqURL:    "https://example.com/",
			reqBody:   new("hello world"),
			body:      true,
			want:      "CUSTOM\x00https\x00\x00example.com\x00/\x00hello world",
		},
		{
			name:      "URL with userinfo",
			reqMethod: "GET",
			//gosec:disable G101 -- Dummy credentials used to test credential handling
			reqURL: "https://foo:bar@example.com/",
			want:   "GET\x00https\x00\x00example.com\x00/",
		},
		{
			name:      "URL with path and query",
			reqMethod: "GET",
			reqURL:    "https://example.com/some/path?with=some&query=parameter",
			want:      "GET\x00https\x00\x00example.com\x00/some/path",
		},
		{
			name:         "unreadable body",
			reqMethod:    "CUSTOM",
			reqURL:       "https://example.com/",
			reqBodyError: io.ErrClosedPipe,
			body:         true,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := http.Request{Method: tt.reqMethod}

			var err error
			if req.URL, err = url.Parse(tt.reqURL); err != nil {
				t.Fatalf("failed to parse reqURL: %s", err)
			}

			if tt.reqBodyError != nil {
				req.Body = io.NopCloser(errReader{tt.reqBodyError})
			} else if tt.reqBody != nil {
				req.Body = io.NopCloser(strings.NewReader(*tt.reqBody))
			}

			got, gotErr := hash(t, &req, tt.body)

			switch {
			case tt.wantErr && gotErr == nil:
				t.Error("HashRequest() err = nil, want non-nil")
			case !tt.wantErr && gotErr != nil:
				t.Errorf("HashRequest() err = %q, want nil", gotErr)
			}

			if got != tt.want && !tt.wantErr {
				t.Errorf("HashRequest() wrote %q, want %q", got, tt.want)
			}

			if tt.reqBody != nil && tt.reqBodyError == nil {
				assertReader(t, "request body after hashing", *tt.reqBody, req.Body)
			}
		})
	}
}

func assertReader(tb testing.TB, prefix, want string, r io.Reader) {
	tb.Helper()

	got, err := io.ReadAll(r)
	if err != nil {
		tb.Errorf("%s: failed to read data from reader: %s", prefix, err)
	}

	if string(got) != want {
		tb.Errorf("%s: got %q, want %q", prefix, string(got), want)
	}
}
