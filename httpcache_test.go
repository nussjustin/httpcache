package httpcache_test

import (
	"maps"
	"math"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/nussjustin/httpcache"
)

func TestParseAge(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{
			name: `basic`,
			in:   `32`,
			want: 32 * time.Second,
		},
		{
			name: `zero`,
			in:   `0`,
		},
		{
			name:    `negative`,
			in:      `-5`,
			wantErr: true,
		},
		{
			name:    `explicit plus`,
			in:      `+5`,
			wantErr: true,
		},
		{
			name:    `float`,
			in:      `1.5`,
			wantErr: true,
		},
		{
			name:    `empty`,
			in:      ``,
			wantErr: true,
		},
		{
			name: `overflow time.Duration`,
			in:   `9223372036854775806`,
			want: time.Duration(math.MaxInt64),
		},
		{
			name: `overflow int64`,
			in:   `9223372037`,
			want: time.Duration(math.MaxInt64),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpcache.ParseAge(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAge() error = %v, want %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseAge() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseExpires(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    time.Time
		wantErr bool
	}{
		{
			name: `basic`,
			in:   `Wed, 21 Oct 2015 07:28:00 GMT`,
			want: time.Date(2015, time.October, 21, 07, 28, 0, 0, time.UTC),
		},
		{
			name:    `non-GMT timezone`,
			in:      `Wed, 21 Oct 2015 07:28:00 CEST`,
			wantErr: true,
		},
		{
			name:    `empty`,
			in:      ``,
			wantErr: true,
		},
		{
			name:    `missing time zone`,
			in:      `Wed, 21 Oct 2015 07:28:00`,
			wantErr: true,
		},
		{
			name:    `missing week day`,
			in:      `21 Oct 2015 07:28:00 GMT`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpcache.ParseExpires(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseExpires() error = %v, want %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseExpires() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_CanStore(t *testing.T) {
	tests := []struct {
		name        string
		config      httpcache.Config
		req         httpcache.Request
		resp        httpcache.Response
		wantPublic  bool
		wantPrivate bool
	}{
		{
			name:        `simple GET`,
			config:      httpcache.Config{},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusOK},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:        `simple HEAD`,
			config:      httpcache.Config{},
			req:         httpcache.Request{Method: "HEAD"},
			resp:        httpcache.Response{StatusCode: http.StatusOK},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:        `simple QUERY`,
			config:      httpcache.Config{},
			req:         httpcache.Request{Method: "QUERY"},
			resp:        httpcache.Response{StatusCode: http.StatusOK},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name:        `invalid method`,
			config:      httpcache.Config{},
			req:         httpcache.Request{Method: "POST"},
			resp:        httpcache.Response{StatusCode: http.StatusOK},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `invalid method, allowed by callback`,
			config: httpcache.Config{
				SupportedRequestMethod: func(method string) bool {
					return method == "POST"
				},
			},
			req:         httpcache.Request{Method: "POST"},
			resp:        httpcache.Response{StatusCode: http.StatusOK},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `invalid method, not allowed by callback`,
			config: httpcache.Config{
				SupportedRequestMethod: func(method string) bool {
					return method == "PUT"
				},
			},
			req:         httpcache.Request{Method: "POST"},
			resp:        httpcache.Response{StatusCode: http.StatusOK},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:        `invalid status code`,
			config:      httpcache.Config{},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusContinue},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:        `status code 206`,
			config:      httpcache.Config{},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusPartialContent},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `status code 206, understood by callback`,
			config: httpcache.Config{
				CanUnderstandResponseCode: func(code int) bool {
					return code == http.StatusPartialContent
				},
			},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusPartialContent},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `status code 206, not understood by callback`,
			config: httpcache.Config{
				CanUnderstandResponseCode: func(code int) bool {
					return code != http.StatusPartialContent
				},
			},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusPartialContent},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:        `status code 304`,
			config:      httpcache.Config{},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusNotModified},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `status code 304, understood by callback`,
			config: httpcache.Config{
				CanUnderstandResponseCode: func(code int) bool {
					return code == http.StatusNotModified
				},
			},
			req: httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header:     http.Header{"Cache-Control": {"public"}},
				StatusCode: http.StatusNotModified,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `status code 304, not understood by callback`,
			config: httpcache.Config{
				CanUnderstandResponseCode: func(code int) bool {
					return code != http.StatusNotModified
				},
			},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusNotModified},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `must-understand`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"must-understand"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `must-understand, understood by callback`,
			config: httpcache.Config{
				CanUnderstandResponseCode: func(code int) bool {
					return code == http.StatusOK
				},
			},
			req: httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"must-understand"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `must-understand, not understood by callback`,
			config: httpcache.Config{
				CanUnderstandResponseCode: func(code int) bool {
					return code != http.StatusOK
				},
			},
			req: httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"must-understand"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `no-store`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"no-store"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `private`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"private"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: true,
		},
		{
			name:   `private, with headers`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"private=header"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: true,
		},
		{
			name:   `private, with headers, RespectPrivateHeaders set`,
			config: httpcache.Config{RespectPrivateHeaders: true},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"private=header"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name:   `authorized`,
			config: httpcache.Config{},
			req: httpcache.Request{
				Header: http.Header{
					"Authorization": {"Bearer foo:bar"},
				},
				Method: "GET",
			},
			resp: httpcache.Response{
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: true,
		},
		{
			name:   `authorized, must-revalidate`,
			config: httpcache.Config{},
			req: httpcache.Request{
				Header: http.Header{
					"Authorization": {"Bearer foo:bar"},
				},
				Method: "GET",
			},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"must-revalidate"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `authorized, public`,
			config: httpcache.Config{},
			req: httpcache.Request{
				Header: http.Header{
					"Authorization": {"Bearer foo:bar"},
				},
				Method: "GET",
			},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"public"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `authorized, s-max-age > 0`,
			config: httpcache.Config{},
			req: httpcache.Request{
				Header: http.Header{
					"Authorization": {"Bearer foo:bar"},
				},
				Method: "GET",
			},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"s-maxage=5"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `authorized, s-max-age == 0`,
			config: httpcache.Config{},
			req: httpcache.Request{
				Header: http.Header{
					"Authorization": {"Bearer foo:bar"},
				},
				Method: "GET",
			},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"s-maxage=0"},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: true,
		},

		{
			name:   `heuristically cacheable status code`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `heuristically cacheable status, allowed by callback`,
			config: httpcache.Config{
				IsHeuristicallyCacheableStatusCode: func(code int) bool {
					return code == http.StatusOK
				},
			},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusOK},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `heuristically cacheable status, not allowed by callback`,
			config: httpcache.Config{
				IsHeuristicallyCacheableStatusCode: func(code int) bool {
					return code != http.StatusOK
				},
			},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusOK},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name:        `non-heuristically cacheable status`,
			config:      httpcache.Config{},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusCreated},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `non-heuristically cacheable status, allowed by callback`,
			config: httpcache.Config{
				IsHeuristicallyCacheableStatusCode: func(code int) bool {
					return code == http.StatusCreated
				},
			},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusCreated},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `non-heuristically cacheable status, not allowed by callback`,
			config: httpcache.Config{
				IsHeuristicallyCacheableStatusCode: func(code int) bool {
					return code != http.StatusCreated
				},
			},
			req:         httpcache.Request{Method: "GET"},
			resp:        httpcache.Response{StatusCode: http.StatusCreated},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `public`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"public"},
				},
				StatusCode: http.StatusCreated, // not heuristically cacheable
			},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name:   `private`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"private"},
				},
				StatusCode: http.StatusCreated, // not heuristically cacheable
			},
			wantPublic:  false,
			wantPrivate: true,
		},

		{
			name:   `expires`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Expires": {"Wed, 21 Oct 2015 07:28:00 GMT"},
				},
				StatusCode: http.StatusCreated, // not heuristically cacheable
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `invalid expires`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Expires": {"Wed, 21 Oct 2015 07:28:00"}, // missing timezone
				},
				StatusCode: http.StatusCreated, // not heuristically cacheable
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `max-age > 0`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"max-age=5"},
				},
				StatusCode: http.StatusCreated, // not heuristically cacheable
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `max-age == 0`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"max-age=0"},
				},
				StatusCode: http.StatusCreated, // not heuristically cacheable
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `s-maxage > 0`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"s-maxage=5"},
				},
				StatusCode: http.StatusCreated, // not heuristically cacheable
			},
			wantPublic:  true,
			wantPrivate: false,
		},
		{
			name:   `s-maxage == 0`,
			config: httpcache.Config{},
			req:    httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				Header: http.Header{
					"Cache-Control": {"s-maxage=0"},
				},
				StatusCode: http.StatusCreated, // not heuristically cacheable
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name: `extension allows caching`,
			config: httpcache.Config{
				CacheableByExtension: func(req httpcache.Request, resp httpcache.Response) bool {
					return true
				},
			},
			req: httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				StatusCode: http.StatusCreated, // not heuristically cacheable
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `extension does not allow caching`,
			config: httpcache.Config{
				CacheableByExtension: func(req httpcache.Request, resp httpcache.Response) bool {
					return false
				},
			},
			req: httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				StatusCode: http.StatusCreated, // not heuristically cacheable
			},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `extension does not allow caching, but caching is allowed otherwise`,
			config: httpcache.Config{
				CacheableByExtension: func(req httpcache.Request, resp httpcache.Response) bool {
					return false
				},
			},
			req: httpcache.Request{Method: "GET"},
			resp: httpcache.Response{
				StatusCode: http.StatusOK, // heuristically cacheable
			},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name:   `request no-store`,
			config: httpcache.Config{},
			req: httpcache.Request{
				Header: http.Header{
					"Cache-Control": {"no-store"},
				},
				Method: "GET",
			},
			resp:        httpcache.Response{StatusCode: http.StatusOK},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name:   `request no-store, IgnoreRequestDirectiveNoStore set`,
			config: httpcache.Config{IgnoreRequestDirectiveNoStore: true},
			req: httpcache.Request{
				Header: http.Header{
					"Cache-Control": {"no-store"},
				},
				Method: "GET",
			},
			resp:        httpcache.Response{StatusCode: http.StatusOK},
			wantPublic:  true,
			wantPrivate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			public := tt.config
			public.Private = false

			if got := public.CanStore(tt.req, tt.resp); got != tt.wantPublic {
				t.Errorf("Config{Private: false}.CanStore() = %v, want %v", got, tt.wantPublic)
			}

			private := tt.config
			private.Private = true

			if got := private.CanStore(tt.req, tt.resp); got != tt.wantPrivate {
				t.Errorf("Config{Private: true}.CanStore() = %v, want %v", got, tt.wantPrivate)
			}
		})
	}
}

func TestConfig_RemoveUnstorableHeaders(t *testing.T) {
	tests := []struct {
		name    string
		config  httpcache.Config
		headers http.Header
		want    http.Header
	}{
		{
			name:   `default`,
			config: httpcache.Config{},
			headers: http.Header{
				"Age":                       {`10`},
				"Cache-Control":             {`max-age=0, private="Extra-Header-1 extra-header-2"`},
				"Connection":                {"close", "age", "Content-Encoding"},
				"Content-Encoding":          {`gzip`},
				"Content-Length":            {`128`},
				"Content-Type":              {`text/plain; charset=utf-8`},
				"Date":                      {`Mon, 02 Jan 2006 15:04:05 GMT`},
				"Extra-Header-1":            {`Extra value 1`, "Extra value 2"},
				"Extra-Header-2":            {`Extra value 3`, "Extra value 4"},
				"Expires":                   {`Mon, 02 Jan 2007 15:04:05 GMT`},
				"Proxy-Authenticate":        {`Basic realm="Dev", charset="UTF-8"`},
				"Proxy-Authentication-Info": {`Test`},
				"Proxy-Authorization":       {`Basic YWxhZGRpbjpvcGVuc2VzYW1l`},
			},
			want: http.Header{
				"Cache-Control":  {`max-age=0, private="Extra-Header-1 extra-header-2"`},
				"Content-Length": {`128`},
				"Content-Type":   {`text/plain; charset=utf-8`},
				"Date":           {`Mon, 02 Jan 2006 15:04:05 GMT`},
				"Extra-Header-1": {`Extra value 1`, "Extra value 2"},
				"Extra-Header-2": {`Extra value 3`, "Extra value 4"},
				"Expires":        {`Mon, 02 Jan 2007 15:04:05 GMT`},
			},
		},

		{
			name:   `RespectPrivateHeaders set`,
			config: httpcache.Config{RespectPrivateHeaders: true},
			headers: http.Header{
				"Age":                       {`10`},
				"Cache-Control":             {`max-age=0, private="Extra-Header-1 extra-header-2"`},
				"Connection":                {"close", "age", "Content-Encoding"},
				"Content-Encoding":          {`gzip`},
				"Content-Length":            {`128`},
				"Content-Type":              {`text/plain; charset=utf-8`},
				"Date":                      {`Mon, 02 Jan 2006 15:04:05 GMT`},
				"Expires":                   {`Mon, 02 Jan 2007 15:04:05 GMT`},
				"Extra-Header-1":            {`Extra value 1`, "Extra value 2"},
				"Extra-Header-2":            {`Extra value 3`, "Extra value 4"},
				"Proxy-Authenticate":        {`Basic realm="Dev", charset="UTF-8"`},
				"Proxy-Authentication-Info": {`Test`},
				"Proxy-Authorization":       {`Basic YWxhZGRpbjpvcGVuc2VzYW1l`},
			},
			want: http.Header{
				"Cache-Control":  {`max-age=0, private="Extra-Header-1 extra-header-2"`},
				"Content-Length": {`128`},
				"Content-Type":   {`text/plain; charset=utf-8`},
				"Date":           {`Mon, 02 Jan 2006 15:04:05 GMT`},
				"Expires":        {`Mon, 02 Jan 2007 15:04:05 GMT`},
			},
		},

		{
			name:   `StoreProxyHeaders set`,
			config: httpcache.Config{StoreProxyHeaders: true},
			headers: http.Header{
				"Age":                       {`10`},
				"Cache-Control":             {`max-age=0, private="Extra-Header-1 extra-header-2"`},
				"Content-Encoding":          {`gzip`},
				"Content-Length":            {`128`},
				"Content-Type":              {`text/plain; charset=utf-8`},
				"Connection":                {"close", "age", "Content-Encoding"},
				"Date":                      {`Mon, 02 Jan 2006 15:04:05 GMT`},
				"Extra-Header-1":            {`Extra value 1`, "Extra value 2"},
				"Extra-Header-2":            {`Extra value 3`, "Extra value 4"},
				"Expires":                   {`Mon, 02 Jan 2007 15:04:05 GMT`},
				"Proxy-Authenticate":        {`Basic realm="Dev", charset="UTF-8"`},
				"Proxy-Authentication-Info": {`Test`},
				"Proxy-Authorization":       {`Basic YWxhZGRpbjpvcGVuc2VzYW1l`},
			},
			want: http.Header{
				"Cache-Control":             {`max-age=0, private="Extra-Header-1 extra-header-2"`},
				"Content-Length":            {`128`},
				"Content-Type":              {`text/plain; charset=utf-8`},
				"Date":                      {`Mon, 02 Jan 2006 15:04:05 GMT`},
				"Expires":                   {`Mon, 02 Jan 2007 15:04:05 GMT`},
				"Extra-Header-1":            {`Extra value 1`, "Extra value 2"},
				"Extra-Header-2":            {`Extra value 3`, "Extra value 4"},
				"Proxy-Authenticate":        {`Basic realm="Dev", charset="UTF-8"`},
				"Proxy-Authentication-Info": {`Test`},
				"Proxy-Authorization":       {`Basic YWxhZGRpbjpvcGVuc2VzYW1l`},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maps.Clone(tt.headers)

			tt.config.RemoveUnstorableHeaders(got)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Config.RemoveUnstorableHeaders() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRequest_Authorized(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
		want   bool
	}{
		{
			name: `no header`,
		},
		{
			name: `empty header`,
			header: http.Header{
				"Authorization": {""},
			},
			want: true,
		},
		{
			name: `non-empty header`,
			header: http.Header{
				"Authorization": {"Bearer test"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httpcache.Request{Header: tt.header}

			got := r.Authorized()
			if got != tt.want {
				t.Errorf("Request.Authorized() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequest_Directives(t *testing.T) {
	tests := []struct {
		name    string
		header  http.Header
		want    httpcache.RequestDirectives
		wantErr bool
	}{
		{
			name: `no header`,
		},
		{
			name: `empty header`,
			header: http.Header{
				"Cache-Control": {""},
			},
		},
		{
			name: `valid`,
			header: http.Header{
				"Cache-Control": {"max-age=5, no-cache, no-transform"},
			},
			want: httpcache.RequestDirectives{
				MaxAge:      5 * time.Second,
				NoCache:     true,
				NoTransform: true,
			},
		},
		{
			name: `invalid`,
			header: http.Header{
				"Cache-Control": {"max-age=abc, no-cache, no-transform"},
			},
			want: httpcache.RequestDirectives{
				NoCache:     true,
				NoTransform: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httpcache.Request{Header: tt.header}

			got, err := r.Directives()
			if (err != nil) != tt.wantErr {
				t.Errorf("Request.Directives() error = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Response.Directives() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResponse_Age(t *testing.T) {
	tests := []struct {
		name    string
		header  http.Header
		want    time.Duration
		wantErr bool
	}{
		{
			name: `no header`,
		},
		{
			name: `empty header`,
			header: http.Header{
				"Age": {""},
			},
			wantErr: true,
		},
		{
			name: `valid age`,
			header: http.Header{
				"Age": {"123"},
			},
			want: 123 * time.Second,
		},
		{
			name: `invalid age`,
			header: http.Header{
				"Age": {"test"},
			},
			wantErr: true,
		},
		{
			name: `multiple values`,
			header: http.Header{
				"Age": {"1234", "1234"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httpcache.Response{Header: tt.header}

			got, err := r.Age()
			if (err != nil) != tt.wantErr {
				t.Errorf("Response.Age() error = %v, want %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Response.Age() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResponse_Directives(t *testing.T) {
	tests := []struct {
		name    string
		header  http.Header
		want    httpcache.ResponseDirectives
		wantErr bool
	}{
		{
			name: `no header`,
		},
		{
			name: `empty header`,
			header: http.Header{
				"Cache-Control": {""},
			},
		},
		{
			name: `valid`,
			header: http.Header{
				"Cache-Control": {"max-age=5, no-cache, no-transform"},
			},
			want: httpcache.ResponseDirectives{
				MaxAge:      5 * time.Second,
				NoCache:     true,
				NoTransform: true,
			},
		},
		{
			name: `invalid`,
			header: http.Header{
				"Cache-Control": {"max-age=abc, no-cache, no-transform"},
			},
			want: httpcache.ResponseDirectives{
				NoCache:     true,
				NoTransform: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httpcache.Response{Header: tt.header}

			got, err := r.Directives()
			if (err != nil) != tt.wantErr {
				t.Errorf("Response.Directives() error = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Response.Directives() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResponse_Expires(t *testing.T) {
	tests := []struct {
		name    string
		header  http.Header
		want    time.Time
		wantErr bool
	}{
		{
			name: `no header`,
		},
		{
			name: `empty header`,
			header: http.Header{
				"Expires": {""},
			},
			wantErr: true,
		},
		{
			name: `valid Expires`,
			header: http.Header{
				"Expires": {`Wed, 21 Oct 2015 07:28:00 GMT`},
			},
			want: time.Date(2015, time.October, 21, 07, 28, 0, 0, time.UTC),
		},
		{
			name: `invalid Expires`,
			header: http.Header{
				"Expires": {"test"},
			},
			wantErr: true,
		},
		{
			name: `multiple values`,
			header: http.Header{
				"Expires": {"Wed, 21 Oct 2015 07:28:00 GMT", "Wed, 21 Oct 2015 07:28:00 GMT"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httpcache.Response{Header: tt.header}

			got, err := r.Expires()
			if (err != nil) != tt.wantErr {
				t.Errorf("Response.Expires() error = %v, want %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Response.Expires() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResponse_Vary(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
		want   []string
	}{
		{
			name: `no header`,
		},
		{
			name: `empty header`,
			header: http.Header{
				"Vary": {},
			},
		},
		{
			name: `single header`,
			header: http.Header{
				"Vary": {" header-3, HEADER-1 ,Header-2 , HeAdEr-4 "},
			},
			want: []string{"Header-1", "Header-2", "Header-3", "Header-4"},
		},
		{
			name: `single header with duplicates`,
			header: http.Header{
				"Vary": {" header-3, HEADER-1 ,Header-2 , HeAdEr-4 , Header-1, Header-3"},
			},
			want: []string{"Header-1", "Header-2", "Header-3", "Header-4"},
		},
		{
			name: `multiple headers`,
			header: http.Header{
				"Vary": {" header-3, HEADER-1", "Header-2 , HeAdEr-4"},
			},
			want: []string{"Header-1", "Header-2", "Header-3", "Header-4"},
		},
		{
			name: `multiple headers with duplicates`,
			header: http.Header{
				"Vary": {" header-3, HEADER-1 ,Header-2", "HeAdEr-4 , Header-1, Header-3"},
			},
			want: []string{"Header-1", "Header-2", "Header-3", "Header-4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httpcache.Response{Header: tt.header}

			got := r.Vary()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Response.Vary() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseRequestDirectives(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    httpcache.RequestDirectives
		wantErr []string
	}{
		{
			name: `empty`,
		},
		{
			name: `minimal`,
			in:   `no-cache`,
			want: httpcache.RequestDirectives{
				NoCache: true,
			},
		},
		{
			name: `full`,
			in:   `max-age=100, max-stale=200, min-fresh=300, no-cache, no-store, no-transform, only-if-cached`,
			want: httpcache.RequestDirectives{
				MaxAge:       100 * time.Second,
				MaxStale:     200 * time.Second,
				MinFresh:     300 * time.Second,
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
			},
		},
		{
			name: `full with extensions`,
			in:   `max-age=100, max-stale=200, min-fresh=300, no-cache, no-store, no-transform, only-if-cached, extra, extra-with-value="test"`,
			want: httpcache.RequestDirectives{
				MaxAge:       100 * time.Second,
				MaxStale:     200 * time.Second,
				MinFresh:     300 * time.Second,
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: "test", HasValue: true},
				},
			},
		},
		{
			name: `extensions only`,
			in:   `extra, extra-with-value="test"`,
			want: httpcache.RequestDirectives{
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: "test", HasValue: true},
				},
			},
		},
		{
			name: `case-insensitive`,
			in:   `MAX-AGE=100, MAX-STALE=200, MIN-FRESH=300, NO-CACHE, NO-STORE, NO-TRANSFORM, ONLY-IF-CACHED`,
			want: httpcache.RequestDirectives{
				MaxAge:       100 * time.Second,
				MaxStale:     200 * time.Second,
				MinFresh:     300 * time.Second,
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
			},
		},
		{
			name: `duplicates`,
			in: `max-age=100, max-stale=200, min-fresh=300, no-cache, no-store, no-transform, only-if-cached, extra, extra-with-value="test", ` +
				`max-age=150, max-stale=250, min-fresh=350, no-cache, no-store, no-transform, only-if-cached, extra, extra-with-value="test2"`,
			want: httpcache.RequestDirectives{
				MaxAge:       150 * time.Second,
				MaxStale:     250 * time.Second,
				MinFresh:     350 * time.Second,
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: "test", HasValue: true},
					{Name: "extra"},
					{Name: "extra-with-value", Value: "test2", HasValue: true},
				},
			},
		},
		{
			name: `invalid max-age`,
			in:   `no-cache, max-age=test, no-store`,
			want: httpcache.RequestDirectives{
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for max-age: invalid value for delta-seconds",
			},
		},
		{
			name: `invalid second max-age`,
			in:   `no-cache, max-age=100, max-age=test, no-store`,
			want: httpcache.RequestDirectives{
				MaxAge:  100 * time.Second,
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for max-age: invalid value for delta-seconds",
			},
		},
		{
			name: `invalid max-stale`,
			in:   `no-cache, max-stale=test, no-store`,
			want: httpcache.RequestDirectives{
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for max-stale: invalid value for delta-seconds",
			},
		},
		{
			name: `invalid second max-stale`,
			in:   `no-cache, max-stale=200, max-stale=test, no-store`,
			want: httpcache.RequestDirectives{
				MaxStale: 200 * time.Second,
				NoCache:  true,
				NoStore:  true,
			},
			wantErr: []string{
				"invalid value for max-stale: invalid value for delta-seconds",
			},
		},
		{
			name: `invalid min-fresh`,
			in:   `no-cache, min-fresh=test, no-store`,
			want: httpcache.RequestDirectives{
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for min-fresh: invalid value for delta-seconds",
			},
		},
		{
			name: `invalid second min-fresh`,
			in:   `no-cache, min-fresh=300, min-fresh=test, no-store`,
			want: httpcache.RequestDirectives{
				MinFresh: 300 * time.Second,
				NoCache:  true,
				NoStore:  true,
			},
			wantErr: []string{
				"invalid value for min-fresh: invalid value for delta-seconds",
			},
		},
		{
			name: `invalid quoted value`,
			in:   `no-cache, extra-with-value="test, no-store`,
			want: httpcache.RequestDirectives{
				NoCache: true,
				NoStore: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra-with-value", Value: `"test`, HasValue: true},
				},
			},
		},
		{
			name: `value-less directive with value`,
			in:   `no-cache=value, no-store="value with spaces"`,
			want: httpcache.RequestDirectives{
				NoCache: true,
				NoStore: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpcache.ParseRequestDirectives(tt.in)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseRequestDirectives() mismatch (-want +got):\n%s", diff)
			}
			if len(tt.wantErr) == 0 && err != nil {
				t.Errorf("ParseRequestDirectives() error = %v, want nil", err)
			}
			if len(tt.wantErr) > 0 {
				var gotErrs []string

				for _, gotErr := range err.(interface{ Unwrap() []error }).Unwrap() {
					gotErrs = append(gotErrs, gotErr.Error())
				}

				if diff := cmp.Diff(tt.wantErr, gotErrs); diff != "" {
					t.Errorf("ParseRequestDirectives() error mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func BenchmarkParseRequestDirectives(b *testing.B) {
	for b.Loop() {
		_, _ = httpcache.ParseRequestDirectives(`max-age=100, max-stale=200, min-fresh=300, no-cache, no-store, no-transform, only-if-cached, extra, extra-with-value="test"`)
	}
}

func TestRequestDirectives_String(t *testing.T) {
	tests := []struct {
		name string
		in   httpcache.RequestDirectives
		want string
	}{
		{
			name: `empty`,
		},
		{
			name: `full`,
			in: httpcache.RequestDirectives{
				MaxAge:       100 * time.Second,
				MaxStale:     200 * time.Second,
				MinFresh:     300 * time.Second,
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
			},
			want: `max-age=100, max-stale=200, min-fresh=300, no-cache, no-store, no-transform, only-if-cached`,
		},
		{
			name: `full with extensions`,
			in: httpcache.RequestDirectives{
				MaxAge:       100 * time.Second,
				MaxStale:     200 * time.Second,
				MinFresh:     300 * time.Second,
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: "test", HasValue: true},
				},
			},
			want: `max-age=100, max-stale=200, min-fresh=300, no-cache, no-store, no-transform, only-if-cached, extra, extra-with-value="test"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.String(); got != tt.want {
				t.Errorf("RequestDirectives.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseResponseDirectives(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    httpcache.ResponseDirectives
		wantErr []string
	}{
		{
			name: `empty`,
		},
		{
			name: `minimal`,
			in:   `no-cache`,
			want: httpcache.ResponseDirectives{
				NoCache: true,
			},
		},
		{
			name: `full`,
			in:   `max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200`,
			want: httpcache.ResponseDirectives{
				MaxAge:          100 * time.Second,
				MustRevalidate:  true,
				MustUnderstand:  true,
				NoCache:         true,
				NoCacheHeaders:  []string{"Header-1", "Header-2"},
				NoStore:         true,
				NoTransform:     true,
				Private:         true,
				PrivateHeaders:  []string{"Header-3", "Header-4"},
				ProxyRevalidate: true,
				Public:          true,
				SMaxAge:         200 * time.Second,
			},
		},
		{
			name: `full with extensions`,
			in:   `max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200, extra, extra-with-value="test"`,
			want: httpcache.ResponseDirectives{
				MaxAge:          100 * time.Second,
				MustRevalidate:  true,
				MustUnderstand:  true,
				NoCache:         true,
				NoCacheHeaders:  []string{"Header-1", "Header-2"},
				NoStore:         true,
				NoTransform:     true,
				Private:         true,
				PrivateHeaders:  []string{"Header-3", "Header-4"},
				ProxyRevalidate: true,
				Public:          true,
				SMaxAge:         200 * time.Second,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: "test", HasValue: true},
				},
			},
		},
		{
			name: `extensions only`,
			in:   `extra, extra-with-value="test"`,
			want: httpcache.ResponseDirectives{
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: "test", HasValue: true},
				},
			},
		},
		{
			name: `case-insensitive`,
			in:   `MAX-AGE=100, MUST-REVALIDATE, MUST-UNDERSTAND, NO-CACHE="HEADER-1 HEADER-2", NO-STORE, NO-TRANSFORM, PRIVATE="HEADER-3 HEADER-4", PROXY-REVALIDATE, PUBLIC, S-MAXAGE=200`,
			want: httpcache.ResponseDirectives{
				MaxAge:          100 * time.Second,
				MustRevalidate:  true,
				MustUnderstand:  true,
				NoCache:         true,
				NoCacheHeaders:  []string{"HEADER-1", "HEADER-2"},
				NoStore:         true,
				NoTransform:     true,
				Private:         true,
				PrivateHeaders:  []string{"HEADER-3", "HEADER-4"},
				ProxyRevalidate: true,
				Public:          true,
				SMaxAge:         200 * time.Second,
			},
		},
		{
			name: `duplicates`,
			in: `max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200, extra, extra-with-value="test", ` +
				`max-age=150, must-revalidate, must-understand, no-cache="Header-5 Header-6", no-store, no-transform, private="Header-7 Header-8", proxy-revalidate, public, s-maxage=250, extra, extra-with-value="test2"`,
			want: httpcache.ResponseDirectives{
				MaxAge:          150 * time.Second,
				MustRevalidate:  true,
				MustUnderstand:  true,
				NoCache:         true,
				NoCacheHeaders:  []string{"Header-5", "Header-6"},
				NoStore:         true,
				NoTransform:     true,
				Private:         true,
				PrivateHeaders:  []string{"Header-7", "Header-8"},
				ProxyRevalidate: true,
				Public:          true,
				SMaxAge:         250 * time.Second,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: "test", HasValue: true},
					{Name: "extra"},
					{Name: "extra-with-value", Value: "test2", HasValue: true},
				},
			},
		},
		{
			name: `invalid max-age`,
			in:   `no-cache, max-age=test, no-store`,
			want: httpcache.ResponseDirectives{
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for max-age: invalid value for delta-seconds",
			},
		},
		{
			name: `invalid second max-age`,
			in:   `no-cache, max-age=100, max-age=test, no-store`,
			want: httpcache.ResponseDirectives{
				MaxAge:  100 * time.Second,
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for max-age: invalid value for delta-seconds",
			},
		},
		{
			name: `invalid s-maxage`,
			in:   `no-cache, s-maxage=test, no-store`,
			want: httpcache.ResponseDirectives{
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for s-maxage: invalid value for delta-seconds",
			},
		},
		{
			name: `invalid second s-maxage`,
			in:   `no-cache, s-maxage=200, s-maxage=test, no-store`,
			want: httpcache.ResponseDirectives{
				NoCache: true,
				NoStore: true,
				SMaxAge: 200 * time.Second,
			},
			wantErr: []string{
				"invalid value for s-maxage: invalid value for delta-seconds",
			},
		},
		{
			name: `invalid quoted value`,
			in:   `no-cache, extra-with-value="test, no-store`,
			want: httpcache.ResponseDirectives{
				NoCache: true,
				NoStore: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra-with-value", Value: `"test`, HasValue: true},
				},
			},
		},
		{
			name: `value-less directive with value`,
			in:   `must-revalidate=value, must-understand="value with spaces"`,
			want: httpcache.ResponseDirectives{
				MustRevalidate: true,
				MustUnderstand: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpcache.ParseResponseDirectives(tt.in)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseResponseDirectives() mismatch (-want +got):\n%s", diff)
			}
			if len(tt.wantErr) == 0 && err != nil {
				t.Errorf("ParseResponseDirectives() error = %v, want nil", err)
			}
			if len(tt.wantErr) > 0 {
				var gotErrs []string

				for _, gotErr := range err.(interface{ Unwrap() []error }).Unwrap() {
					gotErrs = append(gotErrs, gotErr.Error())
				}

				if diff := cmp.Diff(tt.wantErr, gotErrs); diff != "" {
					t.Errorf("ParseResponseDirectives() error mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func BenchmarkParseResponseDirectives(b *testing.B) {
	for b.Loop() {
		_, _ = httpcache.ParseResponseDirectives(`max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200`)
	}
}

func TestResponseDirectives_String(t *testing.T) {
	tests := []struct {
		name string
		in   httpcache.ResponseDirectives
		want string
	}{
		{
			name: `empty`,
		},
		{
			name: `no-cache without value`,
			in: httpcache.ResponseDirectives{
				NoCache: true,
			},
			want: `no-cache`,
		},
		{
			name: `no-cache with token value`,
			in: httpcache.ResponseDirectives{
				NoCache:        true,
				NoCacheHeaders: []string{"test"},
			},
			// Required to be quoted
			want: `no-cache="test"`,
		},
		{
			name: `private without value`,
			in: httpcache.ResponseDirectives{
				Private: true,
			},
			want: `private`,
		},
		{
			name: `private with token value`,
			in: httpcache.ResponseDirectives{
				Private:        true,
				PrivateHeaders: []string{"test"},
			},
			// Required to be quoted
			want: `private="test"`,
		},
		{
			name: `full`,
			in: httpcache.ResponseDirectives{
				MaxAge:          100 * time.Second,
				MustRevalidate:  true,
				MustUnderstand:  true,
				NoCache:         true,
				NoCacheHeaders:  []string{"Header-1", "Header-2"},
				NoStore:         true,
				NoTransform:     true,
				Private:         true,
				PrivateHeaders:  []string{"Header-3", "Header-4"},
				ProxyRevalidate: true,
				Public:          true,
				SMaxAge:         200 * time.Second,
			},
			want: `max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200`,
		},
		{
			name: `full with extensions`,
			in: httpcache.ResponseDirectives{
				MaxAge:          100 * time.Second,
				MustRevalidate:  true,
				MustUnderstand:  true,
				NoCache:         true,
				NoCacheHeaders:  []string{"Header-1", "Header-2"},
				NoStore:         true,
				NoTransform:     true,
				Private:         true,
				PrivateHeaders:  []string{"Header-3", "Header-4"},
				ProxyRevalidate: true,
				Public:          true,
				SMaxAge:         200 * time.Second,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: "test", HasValue: true},
				},
			},
			want: `max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200, extra, extra-with-value="test"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.String(); got != tt.want {
				t.Errorf("ResponseDirectives.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
