package httpcache_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/nussjustin/httpcache"
)

func TestParseNoVarySearch(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    httpcache.URLVariationConfig
		wantErr error
	}{
		{
			name: "empty",
			want: httpcache.URLVariationConfig{VaryOnKeyOrder: true},
		},
		{
			name: "key-order",
			in:   []string{`key-order`},
			want: httpcache.URLVariationConfig{VaryOnKeyOrder: false},
		},
		{
			name: "params",
			in:   []string{`params`},
			want: httpcache.URLVariationConfig{
				ParamsMode: httpcache.NoVarySearchParamsModeParams,
			},
		},
		{
			name: "params list",
			in:   []string{`params=("a" "b" "c")`},
			want: httpcache.URLVariationConfig{
				Params:     []string{`a`, `b`, `c`},
				ParamsMode: httpcache.NoVarySearchParamsModeParams,
			},
		},
		{
			name: "params list and except",
			in:   []string{`params, except=("a" "b" "c")`},
			want: httpcache.URLVariationConfig{
				Params:     []string{`a`, `b`, `c`},
				ParamsMode: httpcache.NoVarySearchParamsModeExcept,
			},
		},

		// Mixed examples
		{
			name: "key-order and params",
			in:   []string{`key-order, params`},
			want: httpcache.URLVariationConfig{
				KeyOrder:   true,
				ParamsMode: httpcache.NoVarySearchParamsModeParams,
			},
		},
		{
			name: "key-order and params list",
			in:   []string{`key-order, params=("a" "b" "c")`},
			want: httpcache.URLVariationConfig{
				KeyOrder:   true,
				Params:     []string{`a`, `b`, `c`},
				ParamsMode: httpcache.NoVarySearchParamsModeParams,
			},
		},
		{
			name: "key-order and params list and except",
			in:   []string{`key-order, params, except=("a" "b" "c")`},
			want: httpcache.URLVariationConfig{
				KeyOrder:   true,
				Params:     []string{`a`, `b`, `c`},
				ParamsMode: httpcache.NoVarySearchParamsModeExcept,
			},
		},

		{
			name: "key-order and params list and except in reverse order",
			in:   []string{`except=("a" "b" "c"), params, key-order`},
			want: httpcache.URLVariationConfig{
				KeyOrder:   true,
				Params:     []string{`a`, `b`, `c`},
				ParamsMode: httpcache.NoVarySearchParamsModeExcept,
			},
		},

		// TODO: Multi line
		// TODO: Multi line, multiple of each

		// TODO: Invalid from https://httpwg.org/http-extensions/draft-ietf-httpbis-no-vary-search.html#name-examples
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := httpcache.ParseNoVarySearch(tt.in)

			if !errors.Is(gotErr, tt.wantErr) {
				t.Fatalf("ParseNoVarySearch() error = %v, want %v", gotErr, tt.wantErr)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseNoVarySearch() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestURLVariationConfig_Equals(t *testing.T) {
	t.Fatal("NIY")
}
