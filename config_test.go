package httpcache_test

import (
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/nussjustin/httpcache"
)

func TestConfig_AllowsStoringResponse(t *testing.T) {
	tests := []struct {
		name        string
		config      httpcache.Config
		req         httpcache.RequestData
		resp        httpcache.ResponseData
		wantPublic  bool
		wantPrivate bool
	}{
		{
			name:   `simple GET`,
			config: httpcache.Config{},
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `simple HEAD`,
			config: httpcache.Config{},
			req:    httpcache.RequestData{Method: "HEAD"},
			resp: httpcache.ResponseData{
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `simple QUERY`,
			config: httpcache.Config{},
			req:    httpcache.RequestData{Method: "QUERY"},
			resp: httpcache.ResponseData{
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},

		{
			name:   `unsupported request method`,
			config: httpcache.Config{},
			req:    httpcache.RequestData{Method: "POST"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "POST"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `invalid status code`,
			config: httpcache.Config{},
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
				StatusCode: http.StatusContinue,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `status code 206, empty config`,
			config: httpcache.Config{},
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
				StatusCode: http.StatusNotModified,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `must-understand, empty config`,
			config: httpcache.Config{},
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{
				Method: "GET",
				Header: http.Header{
					"Authorization": []string{"Bearer test"},
				},
			},
			resp: httpcache.ResponseData{
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: true,
		},
		{
			name:   `authorized, must-revalidate`,
			config: httpcache.Config{},
			req: httpcache.RequestData{
				Method: "GET",
				Header: http.Header{
					"Authorization": []string{"Bearer test"},
				},
			},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{
				Method: "GET",
				Header: http.Header{
					"Authorization": []string{"Bearer test"},
				},
			},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{
				Method: "GET",
				Header: http.Header{
					"Authorization": []string{"Bearer test"},
				},
			},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{
				Method: "GET",
				Header: http.Header{
					"Authorization": []string{"Bearer test"},
				},
			},
			resp: httpcache.ResponseData{
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
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: false,
		},

		{
			name:   `public`,
			config: httpcache.Config{},
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req:    httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
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
			req: httpcache.RequestData{
				Method: "GET",
				Header: http.Header{
					"Cache-Control": []string{"no-store"},
				},
			},
			resp: httpcache.ResponseData{
				StatusCode: http.StatusOK,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name:   `request no-store, RespectRequestDirectiveNoStore set`,
			config: httpcache.Config{RespectRequestDirectiveNoStore: true},
			req: httpcache.RequestData{
				Method: "GET",
				Header: http.Header{
					"Cache-Control": []string{"no-store"},
				},
			},
			resp: httpcache.ResponseData{
				StatusCode: http.StatusOK,
			},
			wantPublic:  false,
			wantPrivate: false,
		},
		{
			name: `cacheable with extension func`,
			config: httpcache.Config{
				CacheableByExtensionFunc: func(req *httpcache.RequestData, resp *httpcache.ResponseData) bool {
					for ext := range resp.ParsedDirectives.Extensions.Named("custom-directive") {
						if ext.Value.Value == "true" {
							return true
						}
					}

					return false
				},
			},
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
				Header: http.Header{
					"Cache-Control": []string{"custom-directive=true"},
				},
				StatusCode: http.StatusInternalServerError,
			},
			wantPublic:  true,
			wantPrivate: true,
		},
		{
			name: `non-cacheable with extension func`,
			config: httpcache.Config{
				CacheableByExtensionFunc: func(req *httpcache.RequestData, resp *httpcache.ResponseData) bool {
					for ext := range resp.ParsedDirectives.Extensions.Named("custom-directive") {
						if ext.Value.Value == "true" {
							return true
						}
					}

					return false
				},
			},
			req: httpcache.RequestData{Method: "GET"},
			resp: httpcache.ResponseData{
				Header: http.Header{
					"Cache-Control": []string{"custom-directive=false"},
				},
				StatusCode: http.StatusInternalServerError,
			},
			wantPublic:  false,
			wantPrivate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			public := tt.config
			public.Private = false

			req := tt.req.Clone()
			req.ParsedDirectives, _ = tt.config.ParseRequestDirectives(tt.req.Header)

			resp := tt.resp.Clone()
			resp.ParsedDirectives, _ = tt.config.ParseResponseDirectives(tt.resp.Header)

			if got := public.AllowsStoringResponse(req, resp); got != tt.wantPublic {
				t.Errorf("Config{Private: false}.AllowsStoringResponse() = %v, want %v", got, tt.wantPublic)
			}

			private := tt.config
			private.Private = true

			req = tt.req.Clone()
			req.ParsedDirectives, _ = tt.config.ParseRequestDirectives(tt.req.Header)

			resp = tt.resp.Clone()
			resp.ParsedDirectives, _ = tt.config.ParseResponseDirectives(tt.resp.Header)

			if got := private.AllowsStoringResponse(req, resp); got != tt.wantPrivate {
				t.Errorf("Config{Private: true}.AllowsStoringResponse() = %v, want %v", got, tt.wantPrivate)
			}
		})
	}
}

func TestConfig_IsSafeMethod(t *testing.T) {
	tests := []struct {
		name   string
		config httpcache.Config
		method string
		want   bool
	}{
		{
			name:   "default logic, GET",
			method: "GET",
			want:   true,
		},
		{
			name:   "default logic, HEAD",
			method: "HEAD",
			want:   true,
		},
		{
			name:   "default logic, OPTIONS",
			method: "OPTIONS",
			want:   true,
		},
		{
			name:   "default logic, POST",
			method: "POST",
		},
		{
			name:   "default logic, PUT",
			method: "PUT",
		},
		{
			name:   "default logic, QUERY",
			method: "QUERY",
			want:   true,
		},
		{
			name:   "default logic, SEARCH",
			method: "SEARCH",
		},
		{
			name:   "default logic, TRACE",
			method: "TRACE",
			want:   true,
		},
		{
			name:   "default logic, lowercase GET",
			method: "get",
			want:   true,
		},
		{
			name:   "default logic, lowercase POST",
			method: "post",
		},
		{
			name: "custom logic, unsafe GET",
			config: httpcache.Config{IsSafeMethodFunc: func(method string) bool {
				return method != "GET"
			}},
			method: "GET",
		},
		{
			name: "custom logic, safe POST",
			config: httpcache.Config{IsSafeMethodFunc: func(method string) bool {
				return method == "POST"
			}},
			method: "POST",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.IsSafeMethod(tt.method)

			if got != tt.want {
				t.Errorf("Config.IsSafeMethod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_ParseRequestDirectives(t *testing.T) {
	tests := []struct {
		name    string
		config  httpcache.Config
		headers http.Header
		want    httpcache.RequestDirectives
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
			want: httpcache.RequestDirectives{NoCache: true},
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
			want: httpcache.RequestDirectives{NoCache: true},
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
			want: httpcache.RequestDirectives{NoCache: true},
		},
		{
			name: "targeted, no targeted set, cache-control header set",
			config: httpcache.Config{
				TargetList: []string{"Target-1", "Target-2"},
			},
			headers: http.Header{
				"Cache-Control": []string{"no-cache"},
			},
			want: httpcache.RequestDirectives{NoCache: true},
		},
		{
			name: "targeted, no targeted set, cache-control header not set",
			config: httpcache.Config{
				TargetList: []string{"Target-1", "Target-2"},
			},
			headers: http.Header{},
			want:    httpcache.RequestDirectives{},
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
			want: httpcache.RequestDirectives{NoCache: true},
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
			want: httpcache.RequestDirectives{NoCache: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.config.ParseRequestDirectives(tt.headers)
			if err != nil {
				t.Fatalf("Config.ParseResponseDirectives() error = %v, want nil", err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Config.ParseResponseDirectives() mismatch (-want +got):\n%s", diff)
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

	run := func(t *testing.T, copyOnChange bool) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				input := tt.headers.Clone()

				got := tt.config.RemoveUnstorableHeaders(input, copyOnChange)

				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("Config.RemoveUnstorableHeaders() mismatch (-want +got):\n%s", diff)
				}

				if copyOnChange {
					if !headerEqual(input, tt.headers) {
						t.Errorf("Config.RemoveUnstorableHeaders() modified original map")
					}

					if headerEqual(got, input) {
						if !mapsIdentical(got, input) {
							t.Error("Config.RemoveUnstorableHeaders() returned new map even though the map was not changed")
						}
					} else {
						if mapsIdentical(got, input) {
							t.Error("Config.RemoveUnstorableHeaders() returned same map even though map was changed")
						}
					}
				} else if !mapsIdentical(got, input) {
					t.Error("Config.RemoveUnstorableHeaders() returned new map")
				}
			})
		}
	}

	t.Run("copy", func(t *testing.T) {
		run(t, true)
	})

	t.Run("no copy", func(t *testing.T) {
		run(t, false)
	})
}
