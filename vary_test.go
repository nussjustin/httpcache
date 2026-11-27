package httpcache_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/nussjustin/httpcache"
)

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
