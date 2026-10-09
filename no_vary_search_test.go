package httpcache_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/nussjustin/httpcache"
	"github.com/nussjustin/httpsfv"
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
			want: httpcache.DefaultURLVariationConfig,
		},

		{
			name: "key-order",
			in:   []string{`key-order`},
			want: httpcache.URLVariationConfig{VaryOnKeyOrder: false, VaryParamsWildcard: true},
		},
		{
			name: "key-order with explicit false value",
			in:   []string{`key-order=?0`},
			want: httpcache.URLVariationConfig{VaryOnKeyOrder: true, VaryParamsWildcard: true},
		},
		{
			name: "key-order with explicit true value",
			in:   []string{`key-order=?1`},
			want: httpcache.URLVariationConfig{VaryOnKeyOrder: false, VaryParamsWildcard: true},
		},
		{
			name:    "key-order with inner list value returns an error",
			in:      []string{`key-order=()`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("key-order must be a boolean"),
		},
		{
			name:    "key-order with invalid item returns an error",
			in:      []string{`key-order=test`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("key-order must be a boolean"),
		},

		{
			name: "params",
			in:   []string{`params=("a" "b" "c")`},
			want: httpcache.URLVariationConfig{NoVaryParams: []string{"a", "b", "c"}, VaryOnKeyOrder: true, VaryParamsWildcard: true},
		},
		{
			name: "params with encoded keys",
			in:   []string{`params=("%C3%A9+%E6%B0%97")`},
			want: httpcache.URLVariationConfig{NoVaryParams: []string{"é 気"}, VaryOnKeyOrder: true, VaryParamsWildcard: true},
		},
		{
			name: "params with empty inner list",
			in:   []string{`params=()`},
			want: httpcache.URLVariationConfig{VaryOnKeyOrder: true, VaryParamsWildcard: true},
		},

		{
			name: "except",
			in:   []string{`except=("a" "b" "c")`},
			want: httpcache.URLVariationConfig{VaryOnKeyOrder: true, VaryParams: []string{"a", "b", "c"}, NoVaryParamsWildcard: true},
		},
		{
			name: "except with encoded keys",
			in:   []string{`except=("%C3%A9+%E6%B0%97")`},
			want: httpcache.URLVariationConfig{VaryParams: []string{"é 気"}, VaryOnKeyOrder: true, NoVaryParamsWildcard: true},
		},
		{
			name: "except with empty inner list",
			in:   []string{`except=()`},
			want: httpcache.URLVariationConfig{NoVaryParamsWildcard: true, VaryOnKeyOrder: true},
		},

		{
			name:    "invalid structured fields",
			in:      []string{`key="`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: httpsfv.ErrInvalidString,
		},

		// Based on examples from https://httpwg.org/http-extensions/draft-ietf-httpbis-no-vary-search.html#name-examples
		{
			name:    "invalid input: key-order expects a boolean, not a string",
			in:      []string{`key-order="not a boolean"`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("key-order must be a boolean"),
		},
		{
			name: "params expects an inner list, not a string", in: []string{`params="not an inner list"`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("params must be an inner-list"),
		},
		{
			name: "params items must be strings (tokens are invalid)", in: []string{`params=(not-a-string)`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("params must all be strings"),
		},
		{
			name: "params expects an inner list, not a boolean", in: []string{`params=?0`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("params must be an inner-list"),
		},
		{
			name: "params expects an inner list, not a boolean", in: []string{`params=?1`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("params must be an inner-list"),
		},
		{
			name: "params and except cannot both be present", in: []string{`params=?1, except=("x")`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("except and params set at the same time"),
		},
		{
			name: "params and except cannot both be present", in: []string{`params=("a"), except=("x")`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("except and params set at the same time"),
		},
		{
			name: "params and except cannot both be present", in: []string{`params=(), except=()`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("except and params set at the same time"),
		},
		{
			name: "except expects an inner list, not a string", in: []string{`except="not an inner list"`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("except must be an inner-list"),
		},
		{
			name: "except items must be strings (tokens are invalid)", in: []string{`except=(not-a-string)`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("except must all be strings"),
		},
		{
			name: "except expects an inner list, not a boolean", in: []string{`except=?1`},
			want:    httpcache.DefaultURLVariationConfig,
			wantErr: errors.New("except must be an inner-list"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := httpcache.ParseNoVarySearch(tt.in)

			if !errors.Is(gotErr, tt.wantErr) && (gotErr == nil || tt.wantErr == nil || tt.wantErr.Error() != gotErr.Error()) {
				t.Errorf("ParseNoVarySearch() error = %v, want %v", gotErr, tt.wantErr)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseNoVarySearch() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestURLVariationConfig_Equals(t *testing.T) {
	tests := []struct {
		name       string
		config     httpcache.URLVariationConfig
		urlA, urlB url.URL
		want       bool
	}{
		{
			name:   "all equal",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			want:   true,
		},
		{
			name:   "all equal with encoded RawPath",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path with spaces", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path with spaces", RawPath: "/path%20with%20spaces", RawQuery: "key1=value1&key2=value2"},
			want:   true,
		},
		{
			name:   "all equal with encoded RawQuery",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path with spaces", RawQuery: "key1=value1&key2=value 2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path with spaces", RawQuery: "key1=value1&key2=value%202"},
			want:   true,
		},

		{
			name:   "different scheme",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "http", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			want:   false,
		},
		{
			name:   "different host",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.de", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			want:   false,
		},
		{
			name:   "different path",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path2", RawQuery: "key1=value1&key2=value2"},
			want:   false,
		},
		{
			name:   "User is ignored",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2", User: url.User("test")},
			want:   true,
		},

		{
			name:   "default config with different query",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1"},
			want:   false,
		},
		{
			name:   "default config with different key order",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key2=value2&key1=value1"},
			want:   false,
		},

		{
			name:   "vary on key order with non-default-config",
			config: httpcache.URLVariationConfig{VaryOnKeyOrder: true},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key2=value2&key1=value1"},
			want:   false,
		},

		{
			name:   "no vary on key order",
			config: httpcache.URLVariationConfig{VaryOnKeyOrder: false},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key2=value2&key1=value1"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key2=value2&key1=value1"},
			want:   true,
		},
		{
			name:   "no vary on key order with different key order",
			config: httpcache.URLVariationConfig{VaryOnKeyOrder: false},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key2=value2&key1=value1"},
			want:   true,
		},
		{
			name:   "no vary on key order with different values",
			config: httpcache.URLVariationConfig{VaryOnKeyOrder: false},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value3"},
			want:   false,
		},

		{
			name:   "no vary with specific params having different values",
			config: httpcache.URLVariationConfig{NoVaryParams: []string{"key2", "key3"}, VaryParamsWildcard: true},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value3"},
			want:   true,
		},
		{
			name:   "no vary with specific params in only one url",
			config: httpcache.URLVariationConfig{NoVaryParams: []string{"key2", "key3"}, VaryParamsWildcard: true},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2&key3=value3"},
			want:   true,
		},
		{
			name:   "no vary with specific params still checks other params",
			config: httpcache.URLVariationConfig{NoVaryParams: []string{"key2", "key3"}, VaryParamsWildcard: true},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=valueX&key2=value2&key3=value3"},
			want:   false,
		},

		{
			name:   "except all ignores different values",
			config: httpcache.URLVariationConfig{NoVaryParamsWildcard: true},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value3"},
			want:   true,
		},
		{
			name:   "except all ignores different params",
			config: httpcache.URLVariationConfig{NoVaryParamsWildcard: true},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key2=value2"},
			want:   true,
		},
		{
			name:   "except specific params having different values",
			config: httpcache.URLVariationConfig{VaryParams: []string{"key2", "key3"}, NoVaryParamsWildcard: true},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value3"},
			want:   false,
		},
		{
			name:   "except specific params ignores other params",
			config: httpcache.URLVariationConfig{VaryParams: []string{"key2", "key3"}, NoVaryParamsWildcard: true},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value3&key2=value2"},
			want:   true,
		},
		{
			name:   "except specific params checks configured that only exist in one url",
			config: httpcache.URLVariationConfig{VaryParams: []string{"key2", "key3"}, NoVaryParamsWildcard: true},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2&key3=value3"},
			want:   false,
		},
		{
			name:   "except specific params ignores non-configured params that only exist in one url",
			config: httpcache.URLVariationConfig{VaryParams: []string{"key2", "key3"}, NoVaryParamsWildcard: true},
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "key1=value1&key2=value2&key4=value4"},
			want:   true,
		},

		// Examples from https://httpwg.org/http-extensions/draft-ietf-httpbis-no-vary-search.html#name-examples-3
		{
			name:   "parsing performs percent-decoding",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a=x"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "%61=%78"},
			want:   true,
		},
		{
			name:   "parsing performs percent-decoding 2",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a=é"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a=%C3%A9"},
			want:   true,
		},
		{
			name:   "an invalid UTF-8 sequence and the literal U+FFFD character are both parsed as U+FFFD",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a=%f6"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a=%ef%bf%bd"},
			want:   true,
		},
		{
			name:   "parsing splits on & and discards empty strings",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a=x&&&&"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a=x"},
			want:   true,
		},
		{
			name:   "both parse as having an empty string value for a",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a="},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a"},
			want:   true,
		},
		{
			name:   "%20 is parsed as U+0020 SPACE",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a=%20"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a= "},
			want:   true,
		},
		{
			name:   "+ is parsed as U+0020 SPACE",
			config: httpcache.DefaultURLVariationConfig,
			urlA:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a=+"},
			urlB:   url.URL{Scheme: "https", Host: "example.com", Path: "/path", RawQuery: "a= "},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := tt.config.Equals(&tt.urlA, &tt.urlB), tt.want; got != want {
				t.Errorf("Equals() = %v, want %v", got, want)
			}
		})
	}
}
