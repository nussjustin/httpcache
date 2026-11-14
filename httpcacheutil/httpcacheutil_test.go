package httpcacheutil_test

import (
	"net/url"
	"testing"

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

func TestHashRequest(t *testing.T) {
	hash := func(tb testing.TB, method string, url_ url.URL, body []byte) (string, error) {
		var h dummyHash

		n, err := httpcacheutil.HashRequest(&h, method, url_, body)
		if err != nil {
			return "", err
		}

		if n != len(h.b) {
			tb.Errorf("HashRequest() n = %d, want %d", n, len(h.b))
		}

		return string(h.b), nil
	}

	tests := []struct {
		name      string
		reqMethod string
		reqURL    string
		reqBody   *string
		want      string
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
			want:      "CUSTOM\x00https\x00\x00example.com\x00/\x00",
		},
		{
			name:      "custom method with body",
			reqMethod: "CUSTOM",
			reqURL:    "https://example.com/",
			reqBody:   new("hello world"),
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url_, err := url.Parse(tt.reqURL)
			if err != nil {
				t.Fatalf("failed to parse reqURL: %s", err)
			}

			var body []byte
			if tt.reqBody != nil {
				body = []byte(*tt.reqBody)
			}

			got, gotErr := hash(t, tt.reqMethod, *url_, body)
			if gotErr != nil {
				t.Errorf("HashRequest() err = %q, want nil", gotErr)
			}

			if got != tt.want {
				t.Errorf("HashRequest() wrote %q, want %q", got, tt.want)
			}
		})
	}
}
