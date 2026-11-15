package httpcacheutil_test

import (
	"net/url"
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
			want:      "GET\x00https\x00\x00example.com\x00443\x00/",
		},
		{
			name:      "HEAD",
			reqMethod: "HEAD",
			reqURL:    "https://example.com/",
			want:      "GET\x00https\x00\x00example.com\x00443\x00/",
		},
		{
			name:      "POST with body",
			reqMethod: "POST",
			reqURL:    "https://example.com/",
			reqBody:   new("hello world"),
			want:      "POST\x00https\x00\x00example.com\x00443\x00/\x00hello world",
		},
		{
			name:      "custom method without body",
			reqMethod: "CUSTOM",
			reqURL:    "https://example.com/",
			want:      "CUSTOM\x00https\x00\x00example.com\x00443\x00/",
		},
		{
			name:      "custom method with empty body",
			reqMethod: "CUSTOM",
			reqURL:    "https://example.com/",
			reqBody:   new(""),
			want:      "CUSTOM\x00https\x00\x00example.com\x00443\x00/\x00",
		},
		{
			name:      "custom method with body",
			reqMethod: "CUSTOM",
			reqURL:    "https://example.com/",
			reqBody:   new("hello world"),
			want:      "CUSTOM\x00https\x00\x00example.com\x00443\x00/\x00hello world",
		},
		{
			name:      "URL with userinfo",
			reqMethod: "GET",
			//gosec:disable G101 -- Dummy credentials used to test credential handling
			reqURL: "https://foo:bar@example.com/",
			want:   "GET\x00https\x00\x00example.com\x00443\x00/",
		},
		{
			name:      "URL with path and query",
			reqMethod: "GET",
			reqURL:    "https://example.com/some/path?with=some&query=parameter",
			want:      "GET\x00https\x00\x00example.com\x00443\x00/some/path",
		},
		{
			name:      "URL with port",
			reqMethod: "GET",
			reqURL:    "https://example.com:1443/",
			want:      "GET\x00https\x00\x00example.com\x001443\x00/",
		},
		{
			name:      "URL with default port",
			reqMethod: "GET",
			reqURL:    "https://example.com:443/",
			want:      "GET\x00https\x00\x00example.com\x00443\x00/",
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

func TestOriginFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     url.URL
		want    httpcacheutil.Origin
		wantErr bool
	}{
		{
			name: "host without port",
			url:  url.URL{Scheme: "https", Host: "example.com", Path: "/"},
			want: httpcacheutil.Origin{Scheme: "https", Host: "example.com", Port: 443},
		},
		{
			name: "host with port",
			url:  url.URL{Scheme: "https", Host: "example.com:1443", Path: "/"},
			want: httpcacheutil.Origin{Scheme: "https", Host: "example.com", Port: 1443},
		},
		{
			name: "IPv4 host",
			url:  url.URL{Scheme: "https", Host: "127.0.0.1", Path: "/"},
			want: httpcacheutil.Origin{Scheme: "https", Host: "127.0.0.1", Port: 443},
		},
		{
			name: "IPv4 host with port",
			url:  url.URL{Scheme: "https", Host: "127.0.0.1:1443", Path: "/"},
			want: httpcacheutil.Origin{Scheme: "https", Host: "127.0.0.1", Port: 1443},
		},
		{
			name: "IPv6 host",
			url:  url.URL{Scheme: "https", Host: "[::1]", Path: "/"},
			want: httpcacheutil.Origin{Scheme: "https", Host: "::1", Port: 443},
		},
		{
			name: "IPv6 host with port",
			url:  url.URL{Scheme: "https", Host: "[::1]:1443", Path: "/"},
			want: httpcacheutil.Origin{Scheme: "https", Host: "::1", Port: 1443},
		},
		{
			name: "non-lowercase scheme",
			url:  url.URL{Scheme: "HTTPS", Host: "example.com", Path: "/"},
			want: httpcacheutil.Origin{Scheme: "https", Host: "example.com", Port: 443},
		},
		{
			name: "port with leading zeroes",
			url:  url.URL{Scheme: "https", Host: "example.com:01443", Path: "/"},
			want: httpcacheutil.Origin{Scheme: "https", Host: "example.com", Port: 1443},
		},
		{
			name: "invalid port",
			url:  url.URL{Scheme: "HTTPS", Host: "example.com:bla", Path: "/"},
			want: httpcacheutil.Origin{Scheme: "https", Host: "example.com:bla", Port: 443},
		},
		{
			name:    "port too large",
			url:     url.URL{Scheme: "HTTPS", Host: "example.com:99999", Path: "/"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := httpcacheutil.OriginFromURL(tt.url)

			if tt.wantErr && gotErr == nil {
				t.Error("OriginFromURL() err = nil, want non-nil")
			} else if !tt.wantErr && gotErr != nil {
				t.Errorf("OriginFromURL() err = %v, want nil", gotErr)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("OriginFromURL() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOrigin_Same(t *testing.T) {
	tests := []struct {
		name    string
		origin1 httpcacheutil.Origin
		origin2 httpcacheutil.Origin
		want    bool
	}{
		{
			name:    "same",
			origin1: httpcacheutil.Origin{Scheme: "https", Host: "example.com", Port: 443},
			origin2: httpcacheutil.Origin{Scheme: "https", Host: "example.com", Port: 443},
			want:    true,
		},
		{
			name:    "different schemes",
			origin1: httpcacheutil.Origin{Scheme: "https", Host: "example.com", Port: 443},
			origin2: httpcacheutil.Origin{Scheme: "http", Host: "example.com", Port: 443},
		},
		{
			name:    "different hosts",
			origin1: httpcacheutil.Origin{Scheme: "https", Host: "example.com", Port: 443},
			origin2: httpcacheutil.Origin{Scheme: "https", Host: "example.org", Port: 443},
		},
		{
			name:    "different ports",
			origin1: httpcacheutil.Origin{Scheme: "https", Host: "example.com", Port: 443},
			origin2: httpcacheutil.Origin{Scheme: "https", Host: "example.com", Port: 1443},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.origin1.Same(tt.origin2)

			if got != tt.want {
				t.Errorf("Origin.Same() = %v, want %v", got, tt.want)
			}

			if got != tt.origin2.Same(tt.origin1) {
				t.Error("Origin.Same() returned different value when swapping arguments")
			}
		})
	}
}
