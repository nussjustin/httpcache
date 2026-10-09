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
	"github.com/nussjustin/httpsfv"
)

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

func TestConfig_ParseResponseDirectives(t *testing.T) {
	tests := []struct {
		name    string
		config  httpcache.Config
		headers http.Header
		want    httpcache.ResponseDirectives
	}{
		{
			name:   "no targeted, no header",
			config: httpcache.Config{},
		},
		{
			name:   "no targeted, cache-control header set",
			config: httpcache.Config{},
			headers: http.Header{
				"Cache-Control": []string{"no-cache"},
			},
			want: httpcache.ResponseDirectives{NoCache: true},
		},
		{
			name: "targeted, all targeted set, cache-control header set",
			config: httpcache.Config{
				TargetList: []string{"Target-1", "Target-2"},
			},
			headers: http.Header{
				"Cache-Control": []string{"no-cache"},
				"Target-1":      []string{"no-store"},
				"Target-2":      []string{"no-transform"},
			},
			want: httpcache.ResponseDirectives{NoStore: true},
		},
		{
			name: "targeted, one targeted set, cache-control header set",
			config: httpcache.Config{
				TargetList: []string{"Target-1", "Target-2"},
			},
			headers: http.Header{
				"Cache-Control": []string{"no-cache"},
				"Target-2":      []string{"no-transform"},
			},
			want: httpcache.ResponseDirectives{NoTransform: true},
		},
		{
			name: "targeted, no targeted set, cache-control header set",
			config: httpcache.Config{
				TargetList: []string{"Target-1", "Target-2"},
			},
			headers: http.Header{
				"Cache-Control": []string{"no-cache"},
			},
			want: httpcache.ResponseDirectives{NoCache: true},
		},
		{
			name: "targeted, no targeted set, cache-control header not set",
			config: httpcache.Config{
				TargetList: []string{"Target-1", "Target-2"},
			},
			headers: http.Header{},
			want:    httpcache.ResponseDirectives{},
		},
		{
			name: "targeted, first targeted invalid, cache-control header set",
			config: httpcache.Config{
				TargetList: []string{"Target-1", "Target-2"},
			},
			headers: http.Header{
				"Cache-Control": []string{"no-cache"},
				"Target-1":      []string{","},
				"Target-2":      []string{"no-transform"},
			},
			want: httpcache.ResponseDirectives{NoTransform: true},
		},
		{
			name: "targeted, all targeted invalid, cache-control header set",
			config: httpcache.Config{
				TargetList: []string{"Target-1", "Target-2"},
			},
			headers: http.Header{
				"Cache-Control": []string{"no-cache"},
				"Target-1":      []string{","},
				"Target-2":      []string{"="},
			},
			want: httpcache.ResponseDirectives{NoCache: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.config.ParseResponseDirectives(tt.headers)

			if err != nil {
				t.Fatalf("Config.ParseResponseDirectives() error = %v, want nil", err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Config.ParseResponseDirectives() mismatch (-want +got):\n%s", diff)
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

func TestVary_Equals(t *testing.T) {
	type args struct {
		vary httpcache.Vary
		h1   http.Header
		h2   http.Header
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "empty",
			want: true,
		},
		{
			name: "matching",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-2"},
				h1: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
				},
				h2: http.Header{
					"Header-1":     []string{"Header-1-Value-1"},
					"Header-2":     []string{"Header-2-Value-1", "Header-2-Value-2"},
					"Extra-Header": []string{"Extra-Header-Value-1"},
				},
			},
			want: true,
		},
		{
			name: "different contents",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-2"},
				h1: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
				},
				h2: http.Header{
					// First character in value has lower case
					"Header-1": []string{"header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
				},
			},
		},
		{
			name: "missing header",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-2"},
				h1: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
				},
				h2: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
				},
			},
		},
		{
			name: "missing header value",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-2"},
				h1: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
				},
				h2: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-2"},
				},
			},
		},
		{
			name: "missing empty header value",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-2"},
				h1: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					// Second value is empty
					"Header-2": []string{"Header-2-Value-1", ""},
				},
				h2: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-2"},
				},
			},
		},
		{
			name: "wrong header value order",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-2"},
				h1: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
				},
				h2: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-2", "Header-2-Value-1"},
				},
			},
		},
		{
			name: "too many values",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-2"},
				h1: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
				},
				h2: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2", "Header-2-Value-2"},
				},
			},
		},

		{
			name: "wildcard",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-2", "*"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.args.vary.Equals(tt.args.h1, tt.args.h2)
			if got != tt.want {
				t.Errorf("Vary.Equals() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVary_Take(t *testing.T) {
	type args struct {
		vary   httpcache.Vary
		header http.Header
	}
	tests := []struct {
		name    string
		args    args
		want    http.Header
		wantNil bool
	}{
		{
			name: "empty",
			args: args{
				header: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
					"Header-3": []string{"Header-3-Value-1"},
				},
			},
			want:    nil,
			wantNil: true,
		},
		{
			name: "all found",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-2"},
				header: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
					"Header-3": []string{"Header-3-Value-1"},
				},
			},
			want: http.Header{
				"Header-1": []string{"Header-1-Value-1"},
				"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
			},
		},
		{
			name: "some found",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-4"},
				header: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
					"Header-3": []string{"Header-3-Value-1"},
				},
			},
			want: http.Header{
				"Header-1": []string{"Header-1-Value-1"},
			},
		},
		{
			name: "none found",
			args: args{
				vary: httpcache.Vary{"Header-4"},
				header: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
					"Header-3": []string{"Header-3-Value-1"},
				},
			},
			want: http.Header{},
		},

		{
			name: "wildcard",
			args: args{
				vary: httpcache.Vary{"Header-1", "header-2", "*"},
				header: http.Header{
					"Header-1": []string{"Header-1-Value-1"},
					"Header-2": []string{"Header-2-Value-1", "Header-2-Value-2"},
					"Header-3": []string{"Header-3-Value-1"},
				},
			},
			want:    nil,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.args.vary.Take(tt.args.header)
			if (got == nil) != tt.wantNil {
				t.Errorf("Vary.Take() got = %v, want nil", got)
			}
			if !headerEqual(got, tt.want) {
				t.Errorf("Vary.Take() got = %v, want %v", got, tt.want)
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
				gotErrs := errorStrings(err)

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

func TestParseTargetedRequestDirectives(t *testing.T) {
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
			name:    `case-insensitive`,
			in:      `MAX-AGE=100, MAX-STALE=200, MIN-FRESH=300, NO-CACHE, NO-STORE, NO-TRANSFORM, ONLY-IF-CACHED`,
			wantErr: []string{httpsfv.ErrInvalidKey.Error()},
		},
		{
			name: `duplicates`,
			in: `max-age=100, max-stale=200, min-fresh=300, no-cache, no-store, no-transform, only-if-cached, extra, extra-with-value="test", ` +
				`max-age=150, max-stale=250, min-fresh=350, no-cache, no-store, no-transform, only-if-cached, extra, extra-with-value="test2"`,
			want: httpcache.RequestDirectives{
				MaxAge:       OptValue(150 * time.Second),
				MaxStale:     OptValue(250 * time.Second),
				MinFresh:     OptValue(350 * time.Second),
				NoCache:      true,
				NoStore:      true,
				NoTransform:  true,
				OnlyIfCached: true,
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test2")},
				},
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
			wantErr: []string{
				"invalid item: invalid bare item: invalid string: missing closing quote",
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
			got, err := httpcache.ParseTargetedRequestDirectives(tt.in)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseTargetedRequestDirectives() mismatch (-want +got):\n%s", diff)
			}
			if len(tt.wantErr) == 0 && err != nil {
				t.Errorf("ParseTargetedRequestDirectives() error = %v, want nil", err)
			}
			if len(tt.wantErr) > 0 {
				gotErrs := errorStrings(err)

				if diff := cmp.Diff(tt.wantErr, gotErrs); diff != "" {
					t.Errorf("ParseTargetedRequestDirectives() error mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func errorStrings(err error) []string {
	uw, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []string{err.Error()}
	}

	var gotErrs []string

	for _, gotErr := range uw.Unwrap() {
		gotErrs = append(gotErrs, gotErr.Error())
	}

	return gotErrs
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

func TestParseTargetedResponseDirectives(t *testing.T) {
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
			name:    `case-insensitive`,
			in:      `MAX-AGE=100, MUST-REVALIDATE, MUST-UNDERSTAND, NO-CACHE="HEADER-1 HEADER-2", NO-STORE, NO-TRANSFORM, PRIVATE="HEADER-3 HEADER-4", PROXY-REVALIDATE, PUBLIC, S-MAXAGE=200`,
			wantErr: []string{httpsfv.ErrInvalidKey.Error()},
		},
		{
			name: `duplicates`,
			in: `max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200, extra, extra-with-value="test", ` +
				`max-age=150, must-revalidate, must-understand, no-cache="Header-5 Header-6", no-store, no-transform, private="Header-7 Header-8", proxy-revalidate, public, s-maxage=250, extra, extra-with-value="test2"`,
			want: httpcache.ResponseDirectives{
				MaxAge:          OptValue(150 * time.Second),
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
				SMaxAge:         OptValue(250 * time.Second),
				Extensions: []httpcache.ExtensionDirective{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test2")},
				},
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
			wantErr: []string{
				"invalid item: invalid bare item: invalid string: missing closing quote",
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
			got, err := httpcache.ParseTargetedResponseDirectives(tt.in)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseTargetedResponseDirectives() mismatch (-want +got):\n%s", diff)
			}
			if len(tt.wantErr) == 0 && err != nil {
				t.Errorf("ParseTargetedResponseDirectives() error = %v, want nil", err)
			}
			if len(tt.wantErr) > 0 {
				gotErrs := errorStrings(err)

				if diff := cmp.Diff(tt.wantErr, gotErrs); diff != "" {
					t.Errorf("ParseTargetedResponseDirectives() error mismatch (-want +got):\n%s", diff)
				}
			}
		})
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

func TestCalculateAge(t *testing.T) {
	reqTime := time.Now()
	respDate := reqTime.Add(1 * time.Second)
	respTime := reqTime.Add(2 * time.Second)
	now := reqTime.Add(3 * time.Second)

	type args struct {
		now      time.Time
		reqTime  time.Time
		respAge  httpcache.Opt[time.Duration]
		respDate time.Time
		respTime time.Time
	}
	tests := []struct {
		name string
		args args
		want time.Duration
	}{
		{
			name: `with explicit age`,
			args: args{
				now:      now,
				reqTime:  reqTime,
				respAge:  OptValue(10 * time.Second),
				respDate: respDate,
				respTime: respTime,
			},
			want: 13 * time.Second,
		},
		{
			name: `without explicit age`,
			args: args{
				now:      now,
				reqTime:  reqTime,
				respAge:  OptValue(5 * time.Second),
				respDate: respDate,
				respTime: respTime,
			},
			want: 8 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := httpcache.CalculateAge(
				tt.args.now,
				tt.args.reqTime,
				tt.args.respAge,
				tt.args.respDate,
				tt.args.respTime,
			); got != tt.want {
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

func TestCalculateFreshnessLifetime(t *testing.T) {
	type args struct {
		privateCache bool
		date         time.Time
		expires      time.Time
		maxAge       httpcache.Opt[time.Duration]
		sMaxAge      httpcache.Opt[time.Duration]
	}
	tests := []struct {
		name   string
		args   args
		want   time.Duration
		wantOk bool
	}{
		{
			name: `no s-maxage, no max-age, no expires`,
			args: args{
				privateCache: true,
			},
		},
		{
			name: `no s-maxage, no max-age, no expires, shared`,
		},

		{
			name: `no s-maxage, no max-age, expires`,
			args: args{
				privateCache: true,
				date:         time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				expires:      time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
			},
			want:   time.Minute,
			wantOk: true,
		},
		{
			name: `no s-maxage, no max-age, expires, shared`,
			args: args{
				date:    time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				expires: time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
			},
			want:   time.Minute,
			wantOk: true,
		},

		{
			name: `no s-maxage, max-age, expires`,
			args: args{
				privateCache: true,
				date:         time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				expires:      time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
				maxAge:       OptValue(5 * time.Second),
			},
			want:   5 * time.Second,
			wantOk: true,
		},
		{
			name: `no s-maxage, max-age, expires, shared`,
			args: args{
				date:    time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				expires: time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
				maxAge:  OptValue(5 * time.Second),
			},
			want:   5 * time.Second,
			wantOk: true,
		},

		{
			name: `s-maxage, max-age, expires`,
			args: args{
				privateCache: true,
				date:         time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				expires:      time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
				maxAge:       OptValue(5 * time.Second),
				sMaxAge:      OptValue(10 * time.Second),
			},
			want:   5 * time.Second,
			wantOk: true,
		},
		{
			name: `s-maxage, max-age, expires, shared`,
			args: args{
				date:    time.Date(2006, time.January, 2, 15, 04, 05, 0, time.UTC),
				expires: time.Date(2006, time.January, 2, 15, 05, 05, 0, time.UTC),
				maxAge:  OptValue(5 * time.Second),
				sMaxAge: OptValue(10 * time.Second),
			},
			want:   10 * time.Second,
			wantOk: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOk := httpcache.CalculateFreshnessLifetime(
				tt.args.privateCache,
				tt.args.date,
				tt.args.expires,
				tt.args.maxAge,
				tt.args.sMaxAge,
			)
			if got != tt.want {
				t.Errorf("FreshnessLifetime() got = %v, want %v", got, tt.want)
			}
			if gotOk != tt.wantOk {
				t.Errorf("FreshnessLifetime() gotOk = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    httpcache.Status
		wantErr bool
	}{
		{
			name: "minimal",
			in:   `test`,
			want: httpcache.Status{
				Cache: "test",
			},
		},
		{
			name: "minimal hit with string cache",
			in:   `"test"`,
			want: httpcache.Status{
				Cache: "test",
			},
		},
		{
			name: "minimal hit",
			in:   `test;hit`,
			want: httpcache.Status{
				Cache: "test",
				Hit:   true,
			},
		},
		{
			name: "full hit",
			in:   `test;hit;ttl=2;key="cache key";detail=detail`,
			want: httpcache.Status{
				Cache:  "test",
				Hit:    true,
				TTL:    2 * time.Second,
				Key:    "cache key",
				Detail: "detail",
			},
		},
		{
			name: "full hit with string detail",
			in:   `test;hit;ttl=2;key="cache key";detail="some detail"`,
			want: httpcache.Status{
				Cache:  "test",
				Hit:    true,
				TTL:    2 * time.Second,
				Key:    "cache key",
				Detail: "some detail",
			},
		},
		{
			name: "minimal forwarded",
			in:   `test;fwd=uri-miss`,
			want: httpcache.Status{
				Cache:     "test",
				Forwarded: httpcache.ForwardedReasonURIMiss,
			},
		},
		{
			name: "full forwarded",
			in:   `test;fwd=vary-miss;fwd-status=418;ttl=2;key="cache key";detail=detail`,
			want: httpcache.Status{
				Cache:           "test",
				Forwarded:       httpcache.ForwardedReasonVaryMiss,
				ForwardedStatus: http.StatusTeapot,
				TTL:             2 * time.Second,
				Key:             "cache key",
				Detail:          "detail",
			},
		},
		{
			name: "full forwarded with string detail",
			in:   `test;fwd=vary-miss;fwd-status=418;ttl=2;key="cache key";detail="some detail"`,
			want: httpcache.Status{
				Cache:           "test",
				Forwarded:       httpcache.ForwardedReasonVaryMiss,
				ForwardedStatus: http.StatusTeapot,
				TTL:             2 * time.Second,
				Key:             "cache key",
				Detail:          "some detail",
			},
		},

		{
			name: "hit set to false",
			in:   `test;hit=?0`,
			want: httpcache.Status{
				Cache: "test",
			},
		},
		{
			name: "explicit true hit",
			in:   `test;hit=?1`,
			want: httpcache.Status{
				Cache: "test",
				Hit:   true,
			},
		},
		{
			name: "negative ttl",
			in:   `test;hit;ttl=-1`,
			want: httpcache.Status{
				Cache: "test",
				Hit:   true,
				TTL:   -time.Second,
			},
		},

		{
			name:    "empty",
			wantErr: true,
		},
		{
			name:    "empty cache",
			in:      `""`,
			wantErr: true,
		},
		{
			name:    "hit and forwarded",
			in:      `test;hit;fwd=miss`,
			wantErr: true,
		},

		{
			name: "invalid",
			// space after parameter key
			in:      `test;fwd =bypass`,
			wantErr: true,
		},
		{
			name:    "invalid cache",
			in:      `%"display string"`,
			wantErr: true,
		},
		{
			name:    "invalid hit",
			in:      `test;hit=token`,
			wantErr: true,
		},
		{
			name:    "invalid fwd",
			in:      `test;fwd="bypass"`,
			wantErr: true,
		},
		{
			name:    "unknown fwd",
			in:      `test;fwd=unknown`,
			wantErr: true,
		},
		{
			name:    "invalid fwd-status",
			in:      `test;fwd=bypass;fwd-status=100.0`,
			wantErr: true,
		},
		{
			name: "unknown fwd-status",
			in:   `test;fwd=bypass;fwd-status=9999`,
			want: httpcache.Status{
				Cache:           "test",
				Forwarded:       httpcache.ForwardedReasonBypass,
				ForwardedStatus: 9999,
			},
		},
		{
			name:    "invalid ttl",
			in:      `test;hit;ttl=2.5`,
			wantErr: true,
		},
		{
			name:    "invalid stored",
			in:      `test;fwd=miss;stored=test`,
			wantErr: true,
		},
		{
			name:    "invalid collapsed",
			in:      `test;fwd=miss;stored=test`,
			wantErr: true,
		},
		{
			name:    "invalid key",
			in:      `test;hit;key=token"`,
			wantErr: true,
		},
		{
			name:    "invalid detail",
			in:      `test;hit;key=%"display string"`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := httpcache.ParseStatus(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseStatus() error = %v, want %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseStatus() got = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestStatus_AppendText(t *testing.T) {
	tests := []struct {
		name    string
		status  httpcache.Status
		want    string
		wantErr bool
	}{
		{
			name: "minimal",
			status: httpcache.Status{
				Cache: "test",
			},
			want: `"test"`,
		},
		{
			name: "minimal hit",
			status: httpcache.Status{
				Cache: "test",
				Hit:   true,
			},
			want: `"test";hit`,
		},
		{
			name: "full hit",
			status: httpcache.Status{
				Cache:  "test",
				Hit:    true,
				TTL:    2*time.Second + 500*time.Millisecond,
				Key:    "cache key",
				Detail: "some detail",
			},
			want: `"test";hit;ttl=2;key="cache key";detail="some detail"`,
		},
		{
			name: "minimal forwarded",
			status: httpcache.Status{
				Cache:     "test",
				Forwarded: httpcache.ForwardedReasonURIMiss,
			},
			want: `"test";fwd=uri-miss`,
		},
		{
			name: "full forwarded",
			status: httpcache.Status{
				Cache:           "test",
				Forwarded:       httpcache.ForwardedReasonVaryMiss,
				ForwardedStatus: http.StatusTeapot,
				TTL:             2*time.Second + 500*time.Millisecond,
				Key:             "cache key",
				Detail:          "some detail",
			},
			want: `"test";fwd=vary-miss;fwd-status=418;ttl=2;key="cache key";detail="some detail"`,
		},

		{
			name: "negative ttl",
			status: httpcache.Status{
				Cache: "test",
				Hit:   true,
				TTL:   -time.Second,
			},
			want: `"test";hit;ttl=-1`,
		},

		{
			name:    "empty",
			wantErr: true,
		},
		{
			name: "hit and forwarded",
			status: httpcache.Status{
				Cache:     "test",
				Hit:       true,
				Forwarded: httpcache.ForwardedReasonBypass,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.status.AppendText([]byte("prefix:"))
			if (err != nil) != tt.wantErr {
				t.Errorf("Status.AppendText() error = %v, want %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if want := "prefix:" + tt.want; string(got) != want {
				t.Errorf("AppendText() got = %q, want %q", got, want)
			}
		})
	}
}
