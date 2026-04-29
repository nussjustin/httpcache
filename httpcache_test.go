package httpcache_test

import (
	"encoding/hex"
	"maps"
	"math"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/nussjustin/httpcache"
)

func TestConfig_AllowsCachedResponseFor(t *testing.T) {
	tests := []struct {
		name   string
		config httpcache.Config
		req    http.Request
		want   bool
	}{
		{
			name:   `supported method, no no-cache directive`,
			config: httpcache.Config{},
			req:    http.Request{Method: "GET"},
			want:   true,
		},
		{
			name:   `supported method, no-cache directive`,
			config: httpcache.Config{},
			req: http.Request{
				Method: "GET",
				Header: http.Header{
					"Cache-Control": []string{"no-cache"},
				},
			},
			want: true,
		},
		{
			name:   `supported method, no-cache directive, RespectRequestDirectiveNoCache set`,
			config: httpcache.Config{RespectRequestDirectiveNoCache: true},
			req: http.Request{
				Method: "GET",
				Header: http.Header{
					"Cache-Control": []string{"no-cache"},
				},
			},
			want: false,
		},
		{
			name:   `unsupported method, no no-cache directive`,
			config: httpcache.Config{},
			req:    http.Request{Method: "POST"},
			want:   false,
		},
		{
			name:   `unsupported method, no-cache directive`,
			config: httpcache.Config{},
			req: http.Request{
				Method: "POST",
				Header: http.Header{
					"Cache-Control": []string{"no-cache"},
				},
			},
			want: false,
		},
		{
			name:   `unsupported method, no-cache directive, RespectRequestDirectiveNoCache set`,
			config: httpcache.Config{RespectRequestDirectiveNoCache: true},
			req: http.Request{
				Method: "POST",
				Header: http.Header{
					"Cache-Control": []string{"no-cache"},
				},
			},
			want: false,
		},
		{
			name:   `supported method by custom list, no no-cache directive`,
			config: httpcache.Config{SupportedRequestMethods: []string{"POST"}},
			req:    http.Request{Method: "POST"},
			want:   true,
		},
		{
			name:   `supported method by custom list, no-cache directive`,
			config: httpcache.Config{SupportedRequestMethods: []string{"POST"}},
			req: http.Request{
				Method: "POST",
				Header: http.Header{
					"Cache-Control": []string{"no-cache"},
				},
			},
			want: true,
		},
		{
			name:   `supported method by custom list, no-cache directive, RespectRequestDirectiveNoCache set`,
			config: httpcache.Config{SupportedRequestMethods: []string{"POST"}, RespectRequestDirectiveNoCache: true},
			req: http.Request{
				Method: "POST",
				Header: http.Header{
					"Cache-Control": []string{"no-cache"},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.AllowsCachedResponseFor(&tt.req); got != tt.want {
				t.Errorf("Config.AllowsCachedResponseFor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_AllowsStoringResponse(t *testing.T) {
	tests := []struct {
		name        string
		config      httpcache.Config
		resp        http.Response
		wantPublic  bool
		wantPrivate bool
	}{
		{
			name:   `simple GET`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `simple HEAD`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "HEAD"},
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `simple QUERY`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "QUERY"},
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name:   `unsupported request method`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "POST"},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `request method supported by custom method list`,
			config: httpcache.Config{
				SupportedRequestMethods: []string{"POST"},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "POST"},
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `request method not supported by custom method list`,
			config: httpcache.Config{
				SupportedRequestMethods: []string{"POST"},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `invalid status code`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusContinue,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `status code 206, empty config`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusPartialContent,
			},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `status code 206, understood`,
			config: httpcache.Config{
				UnderstoodResponseCodes: []int{http.StatusPartialContent},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusPartialContent,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `status code 206, not understood`,
			config: httpcache.Config{
				UnderstoodResponseCodes: []int{http.StatusNotModified},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusPartialContent,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name: `status code 304, empty config`,
			config: httpcache.Config{
				HeuristicallyCacheableStatusCode: []int{http.StatusNotModified},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusNotModified,
			},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `status code 304, understood`,
			config: httpcache.Config{
				HeuristicallyCacheableStatusCode: []int{http.StatusNotModified},
				UnderstoodResponseCodes:          []int{http.StatusNotModified},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusNotModified,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `status code 304, not understood`,
			config: httpcache.Config{
				HeuristicallyCacheableStatusCode: []int{http.StatusNotModified},
				UnderstoodResponseCodes:          []int{http.StatusPartialContent},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusNotModified,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `must-understand, empty config`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"must-understand"},
				},
			},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `must-understand, understood`,
			config: httpcache.Config{
				UnderstoodResponseCodes: []int{http.StatusOK},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"must-understand"},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `must-understand, not understood`,
			config: httpcache.Config{
				UnderstoodResponseCodes: []int{http.StatusNotModified, http.StatusPartialContent},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"must-understand"},
				},
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `no-store`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"no-store"},
				},
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `private`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"private"},
				},
			},
			wantPublic:  false,
			wantPrivate: true,
		},
		{
			name:   `private, with headers`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"private=header"},
				},
			},
			wantPublic:  false,
			wantPrivate: true,
		},
		{
			name:   `private, with headers, RespectResponseDirectivePrivateValue set`,
			config: httpcache.Config{RespectResponseDirectivePrivateValue: true},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"private=header"},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name:   `authorized`,
			config: httpcache.Config{},
			resp: http.Response{
				Request: &http.Request{
					Method: "GET",
					Header: http.Header{
						"Authorization": []string{"Bearer test"},
					},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: true,
		},
		{
			name:   `authorized, must-revalidate`,
			config: httpcache.Config{},
			resp: http.Response{
				Request: &http.Request{
					Method: "GET",
					Header: http.Header{
						"Authorization": []string{"Bearer test"},
					},
				},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"must-revalidate"},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `authorized, public`,
			config: httpcache.Config{},
			resp: http.Response{
				Request: &http.Request{
					Method: "GET",
					Header: http.Header{
						"Authorization": []string{"Bearer test"},
					},
				},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"public"},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `authorized, s-max-age > 0`,
			config: httpcache.Config{},
			resp: http.Response{
				Request: &http.Request{
					Method: "GET",
					Header: http.Header{
						"Authorization": []string{"Bearer test"},
					},
				},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"s-maxage=5"},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `authorized, s-max-age == 0`,
			config: httpcache.Config{},
			resp: http.Response{
				Request: &http.Request{
					Method: "GET",
					Header: http.Header{
						"Authorization": []string{"Bearer test"},
					},
				},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"s-maxage=0"},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name:   `non-heuristically cacheable status`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusCreated,
			},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `heuristically cacheable status by custom list`,
			config: httpcache.Config{
				HeuristicallyCacheableStatusCode: []int{http.StatusCreated},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusCreated,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `non-heuristically cacheable status by custom list`,
			config: httpcache.Config{
				HeuristicallyCacheableStatusCode: []int{http.StatusCreated},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `public`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"public"},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name:   `private`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"private"},
				},
			},
			wantPublic:  false,
			wantPrivate: true,
		},

		{
			name:   `expires`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Expires": []string{"Mon, 02 Jan 2007 15:04:05 GMT"},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `multiple expires`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Expires": []string{
						"Mon, 02 Jan 2007 15:04:05 GMT",
						// Only first should be considered
						"Mon, 03 Jan 2007 15:04:05 INVALID",
					},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name:   `max-age > 0`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"max-age=5"},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `max-age == 0`,
			config: httpcache.Config{},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"max-age=0"},
				},
			},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name: `s-maxage > 0`,
			config: httpcache.Config{
				HeuristicallyCacheableStatusCode: []int{},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"s-maxage=5"},
				},
			},
			wantPublic:  true,
			wantPrivate: false,
		},
		{
			name: `s-maxage == 0`,
			config: httpcache.Config{
				HeuristicallyCacheableStatusCode: []int{},
			},
			resp: http.Response{
				Request:    &http.Request{Method: "GET"},
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": []string{"s-maxage=0"},
				},
			},
			wantPublic:  true,
			wantPrivate: false,
		},

		{
			name:   `request no-store`,
			config: httpcache.Config{},
			resp: http.Response{
				Request: &http.Request{
					Method: "GET",
					Header: http.Header{
						"Cache-Control": []string{"no-store"},
					},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `request no-store, RespectRequestDirectiveNoStore set`,
			config: httpcache.Config{RespectRequestDirectiveNoStore: true},
			resp: http.Response{
				Request: &http.Request{
					Method: "GET",
					Header: http.Header{
						"Cache-Control": []string{"no-store"},
					},
				},
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			public := tt.config
			public.Private = false

			if got := public.AllowsStoringResponse(&tt.resp); got != tt.wantPublic {
				t.Errorf("Config{Private: false}.AllowsStoringResponse() = %v, want %v", got, tt.wantPublic)
			}

			private := tt.config
			private.Private = true

			if got := private.AllowsStoringResponse(&tt.resp); got != tt.wantPrivate {
				t.Errorf("Config{Private: true}.AllowsStoringResponse() = %v, want %v", got, tt.wantPrivate)
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
			name:   `RespectResponseDirectivePrivateValue set`,
			config: httpcache.Config{RespectResponseDirectivePrivateValue: true},
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
			name:    `empty`,
			wantErr: true,
		},
		{
			name: `correct case`,
			in:   `Mon, 02 Jan 2006 15:04:05 GMT`,
			want: time.Date(2006, time.January, 02, 15, 04, 05, 0, time.UTC),
		},
		{
			name: `wrong case`,
			in:   `mON, 02 Jan 2006 15:04:05 gmt`,
			want: time.Date(2006, time.January, 02, 15, 04, 05, 0, time.UTC),
		},
		{
			name:    `invalid day`,
			in:      `Mo, 02 Jan 2006 15:04:05 GMT`,
			wantErr: true,
		},
		{
			name:    `invalid timezone`,
			in:      `Mon, 02 Jan 2006 15:04:05 UTC`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpcache.ParseExpires(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseExpires() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseExpires() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseVary(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want httpcache.Vary
	}{
		{
			name: `no values`,
		},
		{
			name: `empty slice`,
			in:   []string{},
		},
		{
			name: `single header`,
			in:   []string{" header-3, HEADER-1 ,Header-2 , HeAdEr-4 "},
			want: httpcache.Vary{"Header-1", "Header-2", "Header-3", "Header-4"},
		},
		{
			name: `single header with duplicates`,
			in:   []string{" header-3, HEADER-1 ,Header-2 , HeAdEr-4 , Header-1, Header-3"},
			want: httpcache.Vary{"Header-1", "Header-2", "Header-3", "Header-4"},
		},
		{
			name: `multiple headers`,
			in:   []string{" header-3, HEADER-1", "Header-2 , HeAdEr-4"},
			want: httpcache.Vary{"Header-1", "Header-2", "Header-3", "Header-4"},
		},
		{
			name: `multiple headers with duplicates`,
			in:   []string{" header-3, HEADER-1 ,Header-2", "HeAdEr-4 , Header-1, Header-3"},
			want: httpcache.Vary{"Header-1", "Header-2", "Header-3", "Header-4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpcache.ParseVary(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Response.Vary() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVary_Key(t *testing.T) {
	type args struct {
		vary   httpcache.Vary
		header http.Header
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantNil bool
	}{
		{
			name:    `empty`,
			wantNil: true,
		},
		{
			name: `existing header`,
			args: args{
				vary: httpcache.Vary{"Header-1", "Header-2"},
				header: http.Header{
					"Header-1": {"Header-1-Value-1", "Header-1-Value-2"},
					"Header-2": {"Header-2-Value-1", "Header-2-Value-2"},
					"Header-3": {"Header-3-Value-1", "Header-3-Value-3"},
				},
			},
			want: `1775c79cde57a75e2fc3bd35b7e58a74c214dabb`,
		},
		{
			name: `non-existing header`,
			args: args{
				vary: httpcache.Vary{"Header-1", "Header-2"},
				header: http.Header{
					"Header-3": {"Header-3-Value-1", "Header-3-Value-3"},
				},
			},
			want: `b071dc8a0b2b5f8aa80e371a7e446c4b8586f011`,
		},
		{
			name: `mix of existing and non-existing headers`,
			args: args{
				vary: httpcache.Vary{"Header-1", "Header-2"},
				header: http.Header{
					"Header-1": {"Header-1-Value-1", "Header-1-Value-2"},
					"Header-3": {"Header-3-Value-1", "Header-3-Value-3"},
				},
			},
			want: `4d7b2c07922f0a25128d08de082c1a146956bbe5`,
		},
		{
			name: `mix of existing and empty headers`,
			args: args{
				vary: httpcache.Vary{"Header-1", "Header-2"},
				header: http.Header{
					"Header-1": {"Header-1-Value-1", "Header-1-Value-2"},
					"Header-2": {""},
					"Header-3": {"Header-3-Value-1", "Header-3-Value-3"},
				},
			},
			want: `f054b248f9fbf25a9712d9e3dfeb84f087bcff18`,
		},
		{
			name: `wildcard`,
			args: args{
				vary: httpcache.Vary{"*"},
			},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.args.vary.Key(nil, tt.args.header)
			if (got == nil) != tt.wantNil {
				t.Errorf("Vary.Key() got = %x, want nil", string(got))
			}
			encoded := hex.EncodeToString(got)
			if encoded != tt.want {
				t.Errorf("Vary.Key() got = %v, want %v", encoded, tt.want)
			}
		})
	}
}

func TestVary_Wildcard(t *testing.T) {
	tests := []struct {
		name string
		in   httpcache.Vary
		want bool
	}{
		{
			name: `empty`,
		},
		{
			name: `single non-wildcard`,
			in:   httpcache.Vary{"Header-1"},
		},
		{
			name: `multiple non-wildcards`,
			in:   httpcache.Vary{"Header-1", "Header-2", "Header-3"},
		},
		{
			name: `single wildcard`,
			in:   httpcache.Vary{"*"},
			want: true,
		},
		{
			name: `multiple wildcards`,
			in:   httpcache.Vary{"*", "*", "*"},
			want: true,
		},
		{
			name: `mixed`,
			in:   httpcache.Vary{"Header-1", "*", "Header-2"},
			want: true,
		},
		{
			name: `asterisk in header`,
			in:   httpcache.Vary{"Header-*-Name"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.Wildcard()
			if got != tt.want {
				t.Errorf("Vary.Wildcard() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func OptValue[T any](t T) httpcache.Opt[T] {
	return httpcache.Opt[T]{Value: t, Valid: true}
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
				MaxAge:       OptValue(100 * time.Second),
				MaxStale:     OptValue(200 * time.Second),
				MinFresh:     OptValue(300 * time.Second),
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
				MaxAge:       OptValue(100 * time.Second),
				MaxStale:     OptValue(200 * time.Second),
				MinFresh:     OptValue(300 * time.Second),
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
				},
			},
		},
		{
			name: `extensions only`,
			in:   `extra, extra-with-value="test"`,
			want: httpcache.RequestDirectives{
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
				},
			},
		},
		{
			name: `case-insensitive`,
			in:   `MAX-AGE=100, MAX-STALE=200, MIN-FRESH=300, NO-CACHE, NO-STORE, NO-TRANSFORM, ONLY-IF-CACHED`,
			want: httpcache.RequestDirectives{
				MaxAge:       OptValue(100 * time.Second),
				MaxStale:     OptValue(200 * time.Second),
				MinFresh:     OptValue(300 * time.Second),
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
				MaxAge:       OptValue(0 * time.Second),
				MaxStale:     OptValue(0 * time.Second),
				MinFresh:     OptValue(time.Duration(math.MaxInt64)),
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test2")},
				},
			},
			wantErr: []string{
				"conflicting values found for directive max-age",
				"conflicting values found for directive max-stale",
				"conflicting values found for directive min-fresh",
			},
		},
		{
			name: `duplicates with same max-/min- values`,
			in: `max-age=100, max-stale=200, min-fresh=300, no-cache, no-store, no-transform, only-if-cached, extra, extra-with-value="test", ` +
				`max-age=100, max-stale=200, min-fresh=300, no-cache, no-store, no-transform, only-if-cached, extra, extra-with-value="test2"`,
			want: httpcache.RequestDirectives{
				MaxAge:       OptValue(100 * time.Second),
				MaxStale:     OptValue(200 * time.Second),
				MinFresh:     OptValue(300 * time.Second),
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test2")},
				},
			},
		},
		{
			name: `invalid max-age`,
			in:   `no-cache, max-age=test, no-store`,
			want: httpcache.RequestDirectives{
				MaxAge:  OptValue(time.Duration(0)),
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for max-age",
			},
		},
		{
			name: `invalid second max-age`,
			in:   `no-cache, max-age=100, max-age=test, no-store`,
			want: httpcache.RequestDirectives{
				MaxAge:  OptValue(time.Duration(0)),
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for max-age",
			},
		},
		{
			name: `invalid max-stale`,
			in:   `no-cache, max-stale=test, no-store`,
			want: httpcache.RequestDirectives{
				MaxStale: OptValue(time.Duration(0)),
				NoCache:  true,
				NoStore:  true,
			},
			wantErr: []string{
				"invalid value for max-stale",
			},
		},
		{
			name: `invalid second max-stale`,
			in:   `no-cache, max-stale=200, max-stale=test, no-store`,
			want: httpcache.RequestDirectives{
				MaxStale: OptValue(time.Duration(0)),
				NoCache:  true,
				NoStore:  true,
			},
			wantErr: []string{
				"invalid value for max-stale",
			},
		},
		{
			name: `invalid min-fresh`,
			in:   `no-cache, min-fresh=test, no-store`,
			want: httpcache.RequestDirectives{
				MinFresh: OptValue(time.Duration(math.MaxInt64)),
				NoCache:  true,
				NoStore:  true,
			},
			wantErr: []string{
				"invalid value for min-fresh",
			},
		},
		{
			name: `invalid second min-fresh`,
			in:   `no-cache, min-fresh=300, min-fresh=test, no-store`,
			want: httpcache.RequestDirectives{
				MinFresh: OptValue(time.Duration(math.MaxInt64)),
				NoCache:  true,
				NoStore:  true,
			},
			wantErr: []string{
				"invalid value for min-fresh",
			},
		},
		{
			name: `invalid quoted value`,
			in:   `no-cache, extra-with-value="test, no-store`,
			want: httpcache.RequestDirectives{
				NoCache: true,
				NoStore: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra-with-value", Value: OptValue(`"test`)},
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
				MaxAge:       OptValue(100 * time.Second),
				MaxStale:     OptValue(200 * time.Second),
				MinFresh:     OptValue(300 * time.Second),
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
				MaxAge:       OptValue(100 * time.Second),
				MaxStale:     OptValue(200 * time.Second),
				MinFresh:     OptValue(300 * time.Second),
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
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

func TestRequestMetadataFromRequest(t *testing.T) {
	now := time.Now()
	type args struct {
		req http.Request
		at  time.Time
	}
	tests := []struct {
		name    string
		args    args
		want    httpcache.RequestMetadata
		wantErr bool
	}{
		{
			name: `empty values`,
			want: httpcache.RequestMetadata{},
		},
		{
			name: `with authorization header`,
			args: args{
				req: http.Request{
					Method: http.MethodGet,
					Header: http.Header{
						"Authorization": []string{"Test"},
					},
				},
				at: now,
			},
			want: httpcache.RequestMetadata{
				Authorized: true,
				Method:     http.MethodGet,
				Time:       now,
			},
		},
		{
			name: `with empty authorization header`,
			args: args{
				req: http.Request{
					Method: http.MethodGet,
					Header: http.Header{
						"Authorization": []string{""},
					},
				},
				at: now,
			},
			want: httpcache.RequestMetadata{
				Authorized: true,
				Method:     http.MethodGet,
				Time:       now,
			},
		},
		{
			name: `with cache-control`,
			args: args{
				req: http.Request{
					Method: http.MethodGet,
					Header: http.Header{
						"Cache-Control": []string{"max-age=5, no-cache"},
					},
				},
				at: now,
			},
			want: httpcache.RequestMetadata{
				Directives: httpcache.RequestDirectives{
					MaxAge:  OptValue(5 * time.Second),
					NoCache: true,
				},
				Method: http.MethodGet,
				Time:   now,
			},
		},
		{
			name: `with invalid cache-control`,
			args: args{
				req: http.Request{
					Method: http.MethodGet,
					Header: http.Header{
						"Cache-Control": []string{"max-age=test, no-cache"},
					},
				},
				at: now,
			},
			want: httpcache.RequestMetadata{
				Directives: httpcache.RequestDirectives{
					MaxAge:  OptValue(0 * time.Second),
					NoCache: true,
				},
				Method: http.MethodGet,
				Time:   now,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpcache.RequestMetadataFromRequest(&tt.args.req, tt.args.at)
			if (err != nil) != tt.wantErr {
				t.Errorf("RequestMetadataFromRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("RequestMetadataFromRequest() mismatch (-want +got):\n%s", diff)
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
				MaxAge:          OptValue(100 * time.Second),
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
				SMaxAge:         OptValue(200 * time.Second),
			},
		},
		{
			name: `full with extensions`,
			in:   `max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200, extra, extra-with-value="test"`,
			want: httpcache.ResponseDirectives{
				MaxAge:          OptValue(100 * time.Second),
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
				SMaxAge:         OptValue(200 * time.Second),
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
				},
			},
		},
		{
			name: `extensions only`,
			in:   `extra, extra-with-value="test"`,
			want: httpcache.ResponseDirectives{
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
				},
			},
		},
		{
			name: `case-insensitive`,
			in:   `MAX-AGE=100, MUST-REVALIDATE, MUST-UNDERSTAND, NO-CACHE="HEADER-1 HEADER-2", NO-STORE, NO-TRANSFORM, PRIVATE="HEADER-3 HEADER-4", PROXY-REVALIDATE, PUBLIC, S-MAXAGE=200`,
			want: httpcache.ResponseDirectives{
				MaxAge:          OptValue(100 * time.Second),
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
				SMaxAge:         OptValue(200 * time.Second),
			},
		},
		{
			name: `duplicates`,
			in: `max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200, extra, extra-with-value="test", ` +
				`max-age=150, must-revalidate, must-understand, no-cache="Header-5 Header-6", no-store, no-transform, private="Header-7 Header-8", proxy-revalidate, public, s-maxage=250, extra, extra-with-value="test2"`,
			want: httpcache.ResponseDirectives{
				MaxAge:          OptValue(0 * time.Second),
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
				SMaxAge:         OptValue(0 * time.Second),
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test2")},
				},
			},
			wantErr: []string{
				"conflicting values found for directive max-age",
				"conflicting values found for directive s-maxage",
			},
		},
		{
			name: `duplicates duplicates with same max-/min- values`,
			in: `max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200, extra, extra-with-value="test", ` +
				`max-age=100, must-revalidate, must-understand, no-cache="Header-5 Header-6", no-store, no-transform, private="Header-7 Header-8", proxy-revalidate, public, s-maxage=200, extra, extra-with-value="test2"`,
			want: httpcache.ResponseDirectives{
				MaxAge:          OptValue(100 * time.Second),
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
				SMaxAge:         OptValue(200 * time.Second),
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test2")},
				},
			},
		},
		{
			name: `invalid max-age`,
			in:   `no-cache, max-age=test, no-store`,
			want: httpcache.ResponseDirectives{
				MaxAge:  OptValue(time.Duration(0)),
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for max-age",
			},
		},
		{
			name: `invalid second max-age`,
			in:   `no-cache, max-age=100, max-age=test, no-store`,
			want: httpcache.ResponseDirectives{
				MaxAge:  OptValue(time.Duration(0)),
				NoCache: true,
				NoStore: true,
			},
			wantErr: []string{
				"invalid value for max-age",
			},
		},
		{
			name: `invalid s-maxage`,
			in:   `no-cache, s-maxage=test, no-store`,
			want: httpcache.ResponseDirectives{
				NoCache: true,
				NoStore: true,
				SMaxAge: OptValue(time.Duration(0)),
			},
			wantErr: []string{
				"invalid value for s-maxage",
			},
		},
		{
			name: `invalid second s-maxage`,
			in:   `no-cache, s-maxage=200, s-maxage=test, no-store`,
			want: httpcache.ResponseDirectives{
				NoCache: true,
				NoStore: true,
				SMaxAge: OptValue(time.Duration(0)),
			},
			wantErr: []string{
				"invalid value for s-maxage",
			},
		},
		{
			name: `invalid quoted value`,
			in:   `no-cache, extra-with-value="test, no-store`,
			want: httpcache.ResponseDirectives{
				NoCache: true,
				NoStore: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra-with-value", Value: OptValue(`"test`)},
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
				MaxAge:          OptValue(100 * time.Second),
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
				SMaxAge:         OptValue(200 * time.Second),
			},
			want: `max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200`,
		},
		{
			name: `full with extensions`,
			in: httpcache.ResponseDirectives{
				MaxAge:          OptValue(100 * time.Second),
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
				SMaxAge:         OptValue(200 * time.Second),
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
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

func TestResponseMetadataFromResponse(t *testing.T) {
	now := time.Now()

	type args struct {
		resp http.Response
		at   time.Time
	}
	tests := []struct {
		name    string
		args    args
		want    httpcache.ResponseMetadata
		wantErr bool
	}{
		{
			name:    `empty values`,
			wantErr: true,
		},
		{
			name: `minimal response`,
			args: args{
				resp: http.Response{
					Header: http.Header{
						"Date": []string{"Mon, 02 Jan 2006 15:04:05 GMT"},
					},
					StatusCode: http.StatusOK,
				},
				at: now,
			},
			want: httpcache.ResponseMetadata{
				Date:       time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				StatusCode: http.StatusOK,
				Time:       now,
			},
		},
		{
			name: `full response`,
			args: args{
				resp: http.Response{
					Header: http.Header{
						"Age":           []string{"3"},
						"Cache-Control": []string{"max-age=5"},
						"Date":          []string{"Mon, 02 Jan 2006 15:04:05 GMT"},
						"Expires":       []string{"Mon, 03 Jan 2006 15:04:05 GMT"},
						"Vary":          []string{"Header-1", "header-2", "header-1", "*", " Header-3"},
					},
					StatusCode: http.StatusOK,
				},
				at: now,
			},
			want: httpcache.ResponseMetadata{
				Age:  OptValue(3 * time.Second),
				Date: time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				Directives: httpcache.ResponseDirectives{
					MaxAge: OptValue(5 * time.Second),
				},
				Expires:    time.Date(2006, time.January, 3, 15, 04, 05, 0, time.UTC),
				StatusCode: http.StatusOK,
				Time:       now,
				Vary:       []string{"*", "Header-1", "Header-2", "Header-3"},
			},
		},
		{
			name: `invalid age`,
			args: args{
				resp: http.Response{
					Header: http.Header{
						"Age":  []string{"test"},
						"Date": []string{"Mon, 02 Jan 2006 15:04:05 GMT"},
					},
					StatusCode: http.StatusOK,
				},
				at: now,
			},
			want: httpcache.ResponseMetadata{
				Date:       time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				StatusCode: http.StatusOK,
				Time:       now,
			},
			wantErr: true,
		},
		{
			name: `invalid cache-control`,
			args: args{
				resp: http.Response{
					Header: http.Header{
						"Cache-Control": []string{"max-age=test, no-cache"},
						"Date":          []string{"Mon, 02 Jan 2006 15:04:05 GMT"},
					},
					StatusCode: http.StatusOK,
				},
				at: now,
			},
			want: httpcache.ResponseMetadata{
				Date: time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				Directives: httpcache.ResponseDirectives{
					MaxAge:  OptValue(0 * time.Second),
					NoCache: true,
				},
				StatusCode: http.StatusOK,
				Time:       now,
			},
			wantErr: true,
		},
		{
			name: `invalid date`,
			args: args{
				resp: http.Response{
					Header: http.Header{
						"Date": []string{"Monday, 02 Jan 2006 15:04:05 GMT"},
					},
					StatusCode: http.StatusOK,
				},
				at: now,
			},
			want: httpcache.ResponseMetadata{
				StatusCode: http.StatusOK,
				Time:       now,
			},
			wantErr: true,
		},
		{
			name: `invalid expires`,
			args: args{
				resp: http.Response{
					Header: http.Header{
						"Date":    []string{"Mon, 02 Jan 2006 15:04:05 GMT"},
						"Expires": []string{"Monday, 02 Jan 2006 15:04:05 GMT"},
					},
					StatusCode: http.StatusOK,
				},
				at: now,
			},
			want: httpcache.ResponseMetadata{
				Date:       time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				StatusCode: http.StatusOK,
				Time:       now,
			},
			wantErr: true,
		},
		{
			name: `multiple expires`,
			args: args{
				resp: http.Response{
					Header: http.Header{
						"Date": []string{"Mon, 02 Jan 2006 15:04:05 GMT"},
						"Expires": []string{
							"Mon, 02 Jan 2006 15:04:05 GMT",
							"Mon, 03 Jan 2006 15:04:05 GMT",
						},
					},
					StatusCode: http.StatusOK,
				},
				at: now,
			},
			want: httpcache.ResponseMetadata{
				Date:       time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				Expires:    time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				StatusCode: http.StatusOK,
				Time:       now,
			},
		},
		// From https://www.rfc-editor.org/rfc/rfc9111#name-freshness
		//
		// Although all date formats are specified to be case-sensitive, a cache recipient SHOULD match the field
		// value case-insensitively.
		{
			name: `case ignored for expires`,
			args: args{
				resp: http.Response{
					Header: http.Header{
						"Date":    []string{"Mon, 02 Jan 2006 15:04:05 GMT"},
						"Expires": []string{"MON, 02 Jan 2006 15:04:05 gmt"},
					},
					StatusCode: http.StatusOK,
				},
				at: now,
			},
			want: httpcache.ResponseMetadata{
				Date:       time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				Expires:    time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				StatusCode: http.StatusOK,
				Time:       now,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpcache.ResponseMetadataFromResponse(&tt.args.resp, tt.args.at)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResponseMetadataFromResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ResponseMetadataFromResponse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestResponseMetadata_FreshnessLifetime(t *testing.T) {
	tests := []struct {
		name     string
		metadata httpcache.ResponseMetadata
		private  bool
		want     time.Duration
		wantOk   bool
	}{
		{
			name:    `no s-maxage, no max-age, no expires`,
			private: true,
		},
		{
			name: `no s-maxage, no max-age, no expires, shared`,
		},

		{
			name: `no s-maxage, no max-age, expires`,
			metadata: httpcache.ResponseMetadata{
				Date:    time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				Expires: time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
			},
			private: true,
			want:    time.Minute,
			wantOk:  true,
		},
		{
			name: `no s-maxage, no max-age, expires, shared`,
			metadata: httpcache.ResponseMetadata{
				Date:    time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				Expires: time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
			},
			want:   time.Minute,
			wantOk: true,
		},

		{
			name: `no s-maxage, max-age, expires`,
			metadata: httpcache.ResponseMetadata{
				Date: time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				Directives: httpcache.ResponseDirectives{
					MaxAge: OptValue(5 * time.Second),
				},
				Expires: time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
			},
			private: true,
			want:    5 * time.Second,
			wantOk:  true,
		},
		{
			name: `no s-maxage, max-age, expires, shared`,
			metadata: httpcache.ResponseMetadata{
				Date: time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				Directives: httpcache.ResponseDirectives{
					MaxAge: OptValue(5 * time.Second),
				},
				Expires: time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
			},
			want:   5 * time.Second,
			wantOk: true,
		},

		{
			name: `s-maxage, max-age, expires`,
			metadata: httpcache.ResponseMetadata{
				Date: time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				Directives: httpcache.ResponseDirectives{
					MaxAge:  OptValue(5 * time.Second),
					SMaxAge: OptValue(10 * time.Second),
				},
				Expires: time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
			},
			private: true,
			want:    5 * time.Second,
			wantOk:  true,
		},
		{
			name: `s-maxage, max-age, expires, shared`,
			metadata: httpcache.ResponseMetadata{
				Date: time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				Directives: httpcache.ResponseDirectives{
					MaxAge:  OptValue(5 * time.Second),
					SMaxAge: OptValue(10 * time.Second),
				},
				Expires: time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
			},
			want:   10 * time.Second,
			wantOk: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOk := tt.metadata.FreshnessLifetime(tt.private)
			if got != tt.want {
				t.Errorf("FreshnessLifetime() got = %v, want %v", got, tt.want)
			}
			if gotOk != tt.wantOk {
				t.Errorf("FreshnessLifetime() gotOk = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

func TestCalculateAge(t *testing.T) {
	reqTime := time.Now()
	respDate := reqTime.Add(1 * time.Second)
	respTime := reqTime.Add(2 * time.Second)
	now := reqTime.Add(3 * time.Second)

	type args struct {
		req  httpcache.RequestMetadata
		resp httpcache.ResponseMetadata
		now  time.Time
	}
	tests := []struct {
		name string
		args args
		want time.Duration
	}{
		{
			name: `with explicit age`,
			args: args{
				req: httpcache.RequestMetadata{
					Time: reqTime,
				},
				resp: httpcache.ResponseMetadata{
					Age:  OptValue(10 * time.Second),
					Date: respDate,
					Time: respTime,
				},
				now: now,
			},
			want: 13 * time.Second,
		},
		{
			name: `without explicit age`,
			args: args{
				req: httpcache.RequestMetadata{
					Time: reqTime,
				},
				resp: httpcache.ResponseMetadata{
					Age:  OptValue(5 * time.Second),
					Date: respDate,
					Time: respTime,
				},
				now: now,
			},
			want: 8 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := httpcache.CalculateAge(tt.args.req, tt.args.resp, tt.args.now); got != tt.want {
				t.Errorf("CalculateAge() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateFreshness(t *testing.T) {
	type args struct {
		currentAge        time.Duration
		freshnessLifetime time.Duration
		minFresh          httpcache.Opt[time.Duration]
		maxAge            httpcache.Opt[time.Duration]
		maxStale          httpcache.Opt[time.Duration]
	}
	tests := []struct {
		name string
		args args
		want httpcache.Freshness
	}{
		{
			name: `fresh`,
			args: args{
				currentAge:        5 * time.Second,
				freshnessLifetime: 10 * time.Second,
			},
			want: httpcache.FreshnessFresh,
		},
		{
			name: `fresh with max-age`,
			args: args{
				currentAge:        5 * time.Second,
				freshnessLifetime: 10 * time.Second,
				maxAge:            OptValue(5 * time.Second),
			},
			want: httpcache.FreshnessFresh,
		},
		{
			name: `fresh with min-fresh`,
			args: args{
				currentAge:        5 * time.Second,
				freshnessLifetime: 10 * time.Second,
				minFresh:          OptValue(5 * time.Second),
			},
			want: httpcache.FreshnessFresh,
		},

		{
			name: `expired`,
			args: args{
				currentAge:        15 * time.Second,
				freshnessLifetime: 10 * time.Second,
			},
			want: httpcache.FreshnessExpired,
		},
		{
			name: `expired based on max-age`,
			args: args{
				currentAge:        5 * time.Second,
				freshnessLifetime: 10 * time.Second,
				maxAge:            OptValue(1 * time.Second),
			},
			want: httpcache.FreshnessExpired,
		},
		{
			name: `expired based on min-fresh`,
			args: args{
				currentAge:        5 * time.Second,
				freshnessLifetime: 10 * time.Second,
				minFresh:          OptValue(6 * time.Second),
			},
			want: httpcache.FreshnessExpired,
		},
		{
			name: `expired after staleness period`,
			args: args{
				currentAge:        15 * time.Second,
				freshnessLifetime: 10 * time.Second,
				maxStale:          OptValue(4 * time.Second),
			},
			want: httpcache.FreshnessExpired,
		},

		{
			name: `stale`,
			args: args{
				currentAge:        15 * time.Second,
				freshnessLifetime: 10 * time.Second,
				maxStale:          OptValue(5 * time.Second),
			},
			want: httpcache.FreshnessStale,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := httpcache.CalculateFreshness(tt.args.currentAge, tt.args.freshnessLifetime, tt.args.minFresh, tt.args.maxAge, tt.args.maxStale); got != tt.want {
				t.Errorf("CalculateFreshness() = %v, want %v", got, tt.want)
			}
		})
	}
}
