package cachecontrol_test

import (
	"slices"
	"testing"

	"github.com/nussjustin/httpcache/internal/cachecontrol"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []cachecontrol.Directive
	}{
		{
			name: `empty`,
			in:   ``,
		},
		{
			name: `two empty`,
			in:   `,`,
		},
		{
			name: `three empty`,
			in:   `,,`,
		},
		{
			name: `lone equal sign`,
			in:   `=`,
			want: []cachecontrol.Directive{
				{HasValue: true},
			},
		},
		{
			name: `double equal signs`,
			in:   `==`,
			want: []cachecontrol.Directive{
				{Value: `=`, HasValue: true},
			},
		},
		{
			name: `single directive`,
			in:   `private`,
			want: []cachecontrol.Directive{
				{Name: `private`},
			},
		},
		{
			name: `broken quoted string`,
			in:   `"private`,
			want: []cachecontrol.Directive{
				{Name: `"private`},
			},
		},
		{
			name: `quoted directive`,
			in:   `"private"`,
			want: []cachecontrol.Directive{
				{Name: `private`},
			},
		},
		{
			name: `directive followed by empty`,
			in:   `private,`,
			want: []cachecontrol.Directive{
				{Name: `private`},
			},
		},
		{
			name: `empty followed by directive`,
			in:   `,private`,
			want: []cachecontrol.Directive{
				{Name: `private`},
			},
		},
		{
			name: `directive between empty`,
			in:   `,private,`,
			want: []cachecontrol.Directive{
				{Name: `private`},
			},
		},
		{
			name: `multiple directives`,
			in:   `private, no-cache, no-store`,
			want: []cachecontrol.Directive{
				{Name: `private`},
				{Name: `no-cache`},
				{Name: `no-store`},
			},
		},
		{
			name: `multiple directives, last with empty value`,
			in:   `private, no-cache, no-store=`,
			want: []cachecontrol.Directive{
				{Name: `private`},
				{Name: `no-cache`},
				{Name: `no-store`, HasValue: true},
			},
		},
		{
			name: `multiple directives, last with empty quoted value`,
			in:   `private, no-cache, no-store=""`,
			want: []cachecontrol.Directive{
				{Name: `private`},
				{Name: `no-cache`},
				{Name: `no-store`, HasValue: true},
			},
		},
		{
			name: `unquoted value with spaces`,
			in:   `private, no-cache, no-store=header1 header2 header3`,
			want: []cachecontrol.Directive{
				{Name: `private`},
				{Name: `no-cache`},
				{Name: `no-store`, Value: `header1 header2 header3`, HasValue: true},
			},
		},
		{
			name: `broken quoted string value`,
			in:   `private, no-cache, no-store="header1 header2 header3`,
			want: []cachecontrol.Directive{
				{Name: `private`},
				{Name: `no-cache`},
				{Name: `no-store`, Value: `"header1 header2 header3`, HasValue: true},
			},
		},
		{
			name: `quoted string value`,
			in:   `private, no-cache, no-store="header1 header2 header3"`,
			want: []cachecontrol.Directive{
				{Name: `private`},
				{Name: `no-cache`},
				{Name: `no-store`, Value: `header1 header2 header3`, HasValue: true},
			},
		},
		{
			name: `spaces around directives`,
			in:   ` private , no-cache , no-store="header1 header2 header3" `,
			want: []cachecontrol.Directive{
				{Name: `private`},
				{Name: `no-cache`},
				{Name: `no-store`, Value: `header1 header2 header3`, HasValue: true},
			},
		},
		{
			name: `broken quoted string value with trailing space`,
			in:   `directive1, directive2="missing ending quote `,
			want: []cachecontrol.Directive{
				{Name: `directive1`},
				{Name: `directive2`, Value: `"missing ending quote`, HasValue: true},
			},
		},
		{
			name: `broken quoted string value with more directives`,
			in:   `directive1, directive2="missing ending quote, directive3=value`,
			want: []cachecontrol.Directive{
				{Name: `directive1`},
				{Name: `directive2`, Value: `"missing ending quote`, HasValue: true},
				{Name: `directive3`, Value: `value`, HasValue: true},
			},
		},
		{
			name: `directive name with spaces`,
			in:   `no store`,
			want: []cachecontrol.Directive{
				{Name: `no store`},
			},
		},
		{
			name: `directive name with spaces and value`,
			in:   `no store=header1`,
			want: []cachecontrol.Directive{
				{Name: `no store`, Value: `header1`, HasValue: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.Collect(cachecontrol.Parse(tt.in))

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Parse() mismatch (-want +git):\n%s", diff)
			}
		})
	}
}

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		var got int

		for range cachecontrol.Parse(`private, no-cache, no-store="header1 header2 header3"`) {
			got++
		}

		if want := 3; got != want {
			b.Errorf("got %d directives, expected %d", got, want)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(`public"`)
	f.Add(`public, max-age=604800"`)
	f.Add(`public, max-age=604800, immutable"`)
	f.Add(`private, no-cache, no-store="header1 header2 header3`)
	f.Add(`private, no-cache, no-store="header1 header2 header3\ header4"`)
	f.Add(`private, no-cache, no-store="header1 header2 header3\ header4\"`)

	f.Fuzz(func(t *testing.T, input string) {
		for range cachecontrol.Parse(input) {
		}
	})
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []cachecontrol.Token
	}{
		{
			name: `empty`,
			in:   ``,
		},
		{
			name: `one comma`,
			in:   `,`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeComma, Text: `,`, Start: 0, End: 1},
			},
		},
		{
			name: `two commas`,
			in:   `,,`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeComma, Text: `,`, Start: 0, End: 1},
				{Type: cachecontrol.TokenTypeComma, Text: `,`, Start: 1, End: 2},
			},
		},
		{
			name: `equal sign`,
			in:   `=`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 0, End: 1},
			},
		},
		{
			name: `double equal sign`,
			in:   `==`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 0, End: 1},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 1, End: 2},
			},
		},
		{
			name: `directive`,
			in:   `name`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `name`, Start: 0, End: 4},
			},
		},
		{
			name: `directive with empty value`,
			in:   `name=`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `name`, Start: 0, End: 4},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 4, End: 5},
			},
		},
		{
			name: `directive with value`,
			in:   `name=value`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `name`, Start: 0, End: 4},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 4, End: 5},
				{Type: cachecontrol.TokenTypeText, Text: `value`, Start: 5, End: 10},
			},
		},
		{
			name: `directive with empty quoted value`,
			in:   `name=""`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `name`, Start: 0, End: 4},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 4, End: 5},
				{Type: cachecontrol.TokenTypeText, Text: ``, Start: 5, End: 7},
			},
		},
		{
			name: `quoted directive`,
			in:   `"name"`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `name`, Start: 0, End: 6},
			},
		},
		{
			name: `quoted directive with empty value`,
			in:   `"name"=`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `name`, Start: 0, End: 6},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 6, End: 7},
			},
		},
		{
			name: `quoted directive with value`,
			in:   `"name"=value`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `name`, Start: 0, End: 6},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 6, End: 7},
				{Type: cachecontrol.TokenTypeText, Text: `value`, Start: 7, End: 12},
			},
		},
		{
			name: `quoted directive with empty quoted value`,
			in:   `"name"=""`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `name`, Start: 0, End: 6},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 6, End: 7},
				{Type: cachecontrol.TokenTypeText, Text: ``, Start: 7, End: 9},
			},
		},
		{
			name: `quoted directive with quoted value`,
			in:   `"name"="value"`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `name`, Start: 0, End: 6},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 6, End: 7},
				{Type: cachecontrol.TokenTypeText, Text: `value`, Start: 7, End: 14},
			},
		},
		{
			name: `empty quoted directive with empty value`,
			in:   `""=`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: ``, Start: 0, End: 2},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 2, End: 3},
			},
		},
		{
			name: `empty quoted directive with empty quoted value`,
			in:   `""=""`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: ``, Start: 0, End: 2},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 2, End: 3},
				{Type: cachecontrol.TokenTypeText, Text: ``, Start: 3, End: 5},
			},
		},
		{
			name: `empty directive with empty quoted value`,
			in:   `=""`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 0, End: 1},
				{Type: cachecontrol.TokenTypeText, Text: ``, Start: 1, End: 3},
			},
		},
		{
			name: `spaces around tokens`,
			in:   ` na   me = before "value with spaces" after `,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 0, End: 1},
				{Type: cachecontrol.TokenTypeText, Text: `na`, Start: 1, End: 3},
				{Type: cachecontrol.TokenTypeSpace, Text: `   `, Start: 3, End: 6},
				{Type: cachecontrol.TokenTypeText, Text: `me`, Start: 6, End: 8},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 8, End: 9},
				{Type: cachecontrol.TokenTypeEquals, Text: `=`, Start: 9, End: 10},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 10, End: 11},
				{Type: cachecontrol.TokenTypeText, Text: `before`, Start: 11, End: 17},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 17, End: 18},
				{Type: cachecontrol.TokenTypeText, Text: `value with spaces`, Start: 18, End: 37},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 37, End: 38},
				{Type: cachecontrol.TokenTypeText, Text: `after`, Start: 38, End: 43},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 43, End: 44},
			},
		},
		{
			name: `spaces around commas`,
			in:   `multiple, directives ,with, mixed ,`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `multiple`, Start: 0, End: 8},
				{Type: cachecontrol.TokenTypeComma, Text: `,`, Start: 8, End: 9},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 9, End: 10},
				{Type: cachecontrol.TokenTypeText, Text: `directives`, Start: 10, End: 20},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 20, End: 21},
				{Type: cachecontrol.TokenTypeComma, Text: `,`, Start: 21, End: 22},
				{Type: cachecontrol.TokenTypeText, Text: `with`, Start: 22, End: 26},
				{Type: cachecontrol.TokenTypeComma, Text: `,`, Start: 26, End: 27},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 27, End: 28},
				{Type: cachecontrol.TokenTypeText, Text: `mixed`, Start: 28, End: 33},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 33, End: 34},
				{Type: cachecontrol.TokenTypeComma, Text: `,`, Start: 34, End: 35},
			},
		},
		{
			name: `missing end quote`,
			in:   `"missing ending quote`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `"missing`, Start: 0, End: 8},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 8, End: 9},
				{Type: cachecontrol.TokenTypeText, Text: `ending`, Start: 9, End: 15},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 15, End: 16},
				{Type: cachecontrol.TokenTypeText, Text: `quote`, Start: 16, End: 21},
			},
		},
		{
			name: `escape in middle`,
			in:   `back\slash`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `back\slash`, Start: 0, End: 10},
			},
		},
		{
			name: `escape in middle of quoted`,
			in:   `"back\slash"`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `backslash`, Start: 0, End: 12},
			},
		},
		{
			name: `escape in middle of broken quoted`,
			in:   `"back\slash`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `"back\slash`, Start: 0, End: 11},
			},
		},
		{
			name: `multiple escapes in quoted`,
			in:   `"mu\lti\ple\ back\slash\es"`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `multiple backslashes`, Start: 0, End: 27},
			},
		},
		{
			name: `quoted containing only escape`,
			in:   `"\"`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `"\"`, Start: 0, End: 3},
			},
		},
		{
			name: `quoted ending with escape`,
			in:   `"backslash-at-end\"`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `"backslash-at-end\"`, Start: 0, End: 19},
			},
		},
		{
			name: `escape in middle of broken quoted between directives`,
			in:   `before, "back\slash, after`,
			want: []cachecontrol.Token{
				{Type: cachecontrol.TokenTypeText, Text: `before`, Start: 0, End: 6},
				{Type: cachecontrol.TokenTypeComma, Text: `,`, Start: 6, End: 7},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 7, End: 8},
				{Type: cachecontrol.TokenTypeText, Text: `"back\slash`, Start: 8, End: 19},
				{Type: cachecontrol.TokenTypeComma, Text: `,`, Start: 19, End: 20},
				{Type: cachecontrol.TokenTypeSpace, Text: ` `, Start: 20, End: 21},
				{Type: cachecontrol.TokenTypeText, Text: `after`, Start: 21, End: 26},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slices.Collect(cachecontrol.Tokenize(tt.in))

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Tokenize() mismatch (-want +git):\n%s", diff)
			}
		})
	}
}
