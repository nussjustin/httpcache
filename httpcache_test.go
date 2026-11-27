package httpcache_test

import (
	"math"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"
	"unsafe"

	"github.com/google/go-cmp/cmp"
	"github.com/nussjustin/httpsfv"

	"github.com/nussjustin/httpcache"
)

func mapsIdentical[MX, MY ~map[K]V, K comparable, V any](x MX, y MY) bool {
	// Maps in Go are references yet the core language
	// provides no safe way to ask whether they alias.
	type pointer = unsafe.Pointer
	return *(*pointer)(pointer(&x)) == *(*pointer)(pointer(&y))
}

func slicesIdentical[S ~[]V, V any](x S, y S) bool {
	return unsafe.SliceData(x) == unsafe.SliceData(y)
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
			want: time.Date(2006, time.January, 0o2, 15, 0o4, 0o5, 0, time.UTC),
		},
		{
			name: `wrong case`,
			in:   `mON, 02 Jan 2006 15:04:05 gmt`,
			want: time.Date(2006, time.January, 0o2, 15, 0o4, 0o5, 0, time.UTC),
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

func TestExtensionDirectives_Named(t *testing.T) {
	es := httpcache.ExtensionDirectives{
		{Name: "directive-a", Value: OptValue("1")},
		{Name: "directive-b", Value: OptValue("2")},
		{Name: "directive-b", Value: OptValue("3")},
		{Name: "directive-a", Value: OptValue("4")},
		{Name: "directive-c", Value: OptValue("5")},
		{Name: "directive-b", Value: OptValue("7")},
	}

	gotA := slices.Collect(es.Named("directive-a"))
	wantA := httpcache.ExtensionDirectives{
		{Name: "directive-a", Value: OptValue("1")},
		{Name: "directive-a", Value: OptValue("4")},
	}
	if !slices.Equal(gotA, wantA) {
		t.Errorf("Named(%q) = %v, want = %v", "directive-a", gotA, wantA)
	}

	gotB := slices.Collect(es.Named("directive-b"))
	wantB := httpcache.ExtensionDirectives{
		{Name: "directive-b", Value: OptValue("2")},
		{Name: "directive-b", Value: OptValue("3")},
		{Name: "directive-b", Value: OptValue("7")},
	}
	if !slices.Equal(gotB, wantB) {
		t.Errorf("Named(%q) = %v, want = %v", "directive-b", gotB, wantB)
	}

	gotC := slices.Collect(es.Named("directive-c"))
	wantC := httpcache.ExtensionDirectives{
		{Name: "directive-c", Value: OptValue("5")},
	}
	if !slices.Equal(gotC, wantC) {
		t.Errorf("Named(%q) = %v, want = %v", "directive-c", gotC, wantC)
	}

	gotD := slices.Collect(es.Named("directive-d"))
	wantD := httpcache.ExtensionDirectives{}
	if !slices.Equal(gotD, wantD) {
		t.Errorf("Named(%q) = %v, want = %v", "directive-d", gotD, wantD)
	}
}

func OptValue[T any](t T) httpcache.Opt[T] {
	return httpcache.Opt[T]{Value: t, Valid: true}
}

func TestRequestData_Clone(t *testing.T) {
	orig := &httpcache.RequestData{
		Time:   time.Now(),
		Method: "GET",
		URL:    &url.URL{Scheme: "https", Host: "example.com", Path: "/", RawQuery: "a=1&b=2"},
		Header: http.Header{
			"Header-1": []string{"Value-1"},
			"Header-2": []string{"Value-2", "Value-3"},
		},
		Body: []byte("request body"),
		ParsedDirectives: httpcache.RequestDirectives{
			NoCache: true,
			NoStore: true,
			Extensions: httpcache.ExtensionDirectives{
				{Name: "Extension-1"},
				{Name: "Extension-2", Value: httpcache.Opt[string]{Value: "Value", Valid: true}},
			},
		},
	}

	cloned := orig.Clone()

	if diff := cmp.Diff(orig, cloned); diff != "" {
		t.Errorf("RequestData.Clone() mismatch (-want +got):\n%s", diff)
	}

	if orig.URL == cloned.URL {
		t.Errorf("RequestData.Clone() returned same *url.URL")
	}

	if mapsIdentical(orig.Header, cloned.Header) {
		t.Errorf("RequestData.Clone() returned same Header map")
	}

	for k := range orig.Header {
		if got, want := orig.Header[k], cloned.Header[k]; slicesIdentical(got, want) {
			t.Errorf("RequestData.Clone() returned same Header %q slice", k)
		}
	}

	if slicesIdentical(orig.ParsedDirectives.Extensions, cloned.ParsedDirectives.Extensions) {
		t.Errorf("RequestData.Clone() returned same ExtensionDirectives slice")
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
				Extensions: httpcache.ExtensionDirectives{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
				},
			},
		},
		{
			name: `extensions only`,
			in:   `extra, extra-with-value="test"`,
			want: httpcache.RequestDirectives{
				Extensions: httpcache.ExtensionDirectives{
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
				Extensions: httpcache.ExtensionDirectives{
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
				Extensions: httpcache.ExtensionDirectives{
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
				Extensions: httpcache.ExtensionDirectives{
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
	b.ReportAllocs()

	for b.Loop() {
		_, _ = httpcache.ParseRequestDirectives(`max-age=100, max-stale=200, min-fresh=300, no-cache, no-store, no-transform, only-if-cached, extra, extra-with-value="test"`)
	}
}

func TestParseStructuredRequestDirectives(t *testing.T) {
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
				Extensions: httpcache.ExtensionDirectives{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
				},
			},
		},
		{
			name: `extensions only`,
			in:   `extra, extra-with-value="test"`,
			want: httpcache.RequestDirectives{
				Extensions: httpcache.ExtensionDirectives{
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
				Extensions: httpcache.ExtensionDirectives{
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
				Extensions: httpcache.ExtensionDirectives{
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
				"invalid item",
				"invalid bare item: invalid string: missing closing quote",
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
			got, err := httpcache.ParseStructuredRequestDirectives(tt.in)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseStructuredRequestDirectives() mismatch (-want +got):\n%s", diff)
			}
			if len(tt.wantErr) == 0 && err != nil {
				t.Errorf("ParseStructuredRequestDirectives() error = %v, want nil", err)
			}
			if len(tt.wantErr) > 0 {
				gotErrs := errorStrings(err)

				if diff := cmp.Diff(tt.wantErr, gotErrs); diff != "" {
					t.Errorf("ParseStructuredRequestDirectives() error mismatch (-want +got):\n%s", diff)
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
				Extensions: httpcache.ExtensionDirectives{
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

func TestResponseData_Age(t *testing.T) {
	tests := []struct {
		name string
		resp func(time.Time) httpcache.ResponseData
		want time.Duration
	}{
		{
			name: "no Date header",
			resp: func(time.Time) httpcache.ResponseData {
				return httpcache.ResponseData{}
			},
			want: time.Minute,
		},
		{
			name: "single Date header",
			resp: func(now time.Time) httpcache.ResponseData {
				return httpcache.ResponseData{
					Header: http.Header{
						"Date": []string{now.Add(-5 * time.Minute).UTC().Format(http.TimeFormat)},
					},
				}
			},
			want: 6 * time.Minute,
		},
		{
			name: "invalid Date header",
			resp: func(time.Time) httpcache.ResponseData {
				return httpcache.ResponseData{
					Header: http.Header{
						"Date": []string{"invalid"},
					},
				}
			},
			want: time.Minute,
		},
		{
			name: "multiple Date headers",
			resp: func(now time.Time) httpcache.ResponseData {
				return httpcache.ResponseData{
					Header: http.Header{
						"Date": []string{
							now.Add(-5 * time.Minute).Format(http.TimeFormat),
							now.Add(-10 * time.Minute).Format(http.TimeFormat),
						},
					},
				}
			},
			want: time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()

				resp := tt.resp(now)
				resp.Time = now

				synctest.Sleep(time.Minute)

				if got := resp.Age(); got != tt.want {
					t.Errorf("ResponseData.Age() = %s, want %s", got, tt.want)
				}
			})
		})
	}
}

func TestResponseData_CacheGroups(t *testing.T) {
	tests := []struct {
		name    string
		resp    httpcache.ResponseData
		want    []string
		wantErr bool
	}{
		{
			name: "no Cache-Groups header",
			resp: httpcache.ResponseData{},
		},
		{
			name: "single Cache-Groups header",
			resp: httpcache.ResponseData{Header: http.Header{
				"Cache-Groups": []string{`"group-a", "group-b", "group-c", "group-b"`},
			}},
			want: []string{"group-a", "group-b", "group-c", "group-b"},
		},
		{
			name: "multiple Cache-Groups headers",
			resp: httpcache.ResponseData{
				Header: http.Header{
					"Cache-Groups": []string{
						`"group-a", "group-b", "group-c", "group-b"`,
						`"group-c"`,
						`"group-a", "group-d"`,
					},
				},
			},
			want: []string{"group-a", "group-b", "group-c", "group-b", "group-c", "group-a", "group-d"},
		},
		{
			name: "parameters are ignored",
			resp: httpcache.ResponseData{Header: http.Header{
				"Cache-Groups": []string{`"group-a";parameter=value, "group-b"`},
			}},
			want: []string{"group-a", "group-b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				got, gotErr := tt.resp.CacheGroups()
				if !slices.Equal(got, tt.want) {
					t.Errorf("ResponseData.CacheGroups() = %v, want %v", got, tt.want)
				}

				if gotErr != nil && !tt.wantErr {
					t.Errorf("ResponseData.CacheGroups() err = %v, want nil", gotErr)
				} else if gotErr == nil && tt.wantErr {
					t.Error("ResponseData.CacheGroups() err = nil, want non-nil")
				}
			})
		})
	}
}

func TestResponseData_Clone(t *testing.T) {
	orig := &httpcache.ResponseData{
		Time:       time.Now(),
		Status:     "Some status",
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Header-1": []string{"Value-1"},
			"Header-2": []string{"Value-2", "Value-3"},
		},
		Body: []byte("request body"),
		Trailer: http.Header{
			"Trailer-1": []string{"Value-4"},
			"Trailer-2": []string{"Value-5", "Value-6"},
		},
		ParsedDirectives: httpcache.ResponseDirectives{
			NoCache: true,
			NoStore: true,
			Extensions: httpcache.ExtensionDirectives{
				{Name: "Extension-1"},
				{Name: "Extension-2", Value: httpcache.Opt[string]{Value: "Value", Valid: true}},
			},
		},
	}

	cloned := orig.Clone()

	if diff := cmp.Diff(orig, cloned); diff != "" {
		t.Errorf("ResponseData.Clone() mismatch (-want +got):\n%s", diff)
	}

	if mapsIdentical(orig.Header, cloned.Header) {
		t.Errorf("ResponseData.Clone() returned same Header map")
	}

	for k := range orig.Header {
		if got, want := orig.Header[k], cloned.Header[k]; slicesIdentical(got, want) {
			t.Errorf("ResponseData.Clone() returned same Header %q slice", k)
		}
	}

	if mapsIdentical(orig.Trailer, cloned.Trailer) {
		t.Errorf("ResponseData.Clone() returned same Trailer map")
	}

	for k := range orig.Trailer {
		if got, want := orig.Trailer[k], cloned.Trailer[k]; slicesIdentical(got, want) {
			t.Errorf("ResponseData.Clone() returned same Trailer %q slice", k)
		}
	}

	if slicesIdentical(orig.ParsedDirectives.Extensions, cloned.ParsedDirectives.Extensions) {
		t.Errorf("ResponseData.Clone() returned same ExtensionDirectives slice")
	}
}

func TestResponseData_Date(t *testing.T) {
	awesomeDate := time.Date(2006, time.January, 2, 15, 4, 5, 0, time.UTC)

	tests := []struct {
		name   string
		resp   httpcache.ResponseData
		want   time.Time
		wantOk bool
	}{
		{
			name: "no Date header",
			resp: httpcache.ResponseData{},
		},
		{
			name: "single Date header",
			resp: httpcache.ResponseData{
				Header: http.Header{
					"Date": []string{awesomeDate.Format(http.TimeFormat)},
				},
			},
			want:   awesomeDate,
			wantOk: true,
		},
		{
			name: "invalid Date header",
			resp: httpcache.ResponseData{
				Header: http.Header{
					"Date": []string{"invalid"},
				},
			},
		},
		{
			name: "multiple Date headers",
			resp: httpcache.ResponseData{
				Header: http.Header{
					"Date": []string{
						awesomeDate.Format(http.TimeFormat),
						awesomeDate.Add(time.Hour).Format(http.TimeFormat),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOk := tt.resp.Date()
			if got != tt.want {
				t.Errorf("ResponseData.Date() = %s, want %s", got, tt.want)
			}
			if gotOk != tt.wantOk {
				t.Errorf("ResponseData.Date() ok = %v, want %v", gotOk, tt.wantOk)
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
				Extensions: httpcache.ExtensionDirectives{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
				},
			},
		},
		{
			name: `extensions only`,
			in:   `extra, extra-with-value="test"`,
			want: httpcache.ResponseDirectives{
				Extensions: httpcache.ExtensionDirectives{
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
				Extensions: httpcache.ExtensionDirectives{
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
			name: `duplicates with same max-/min- values`,
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
				Extensions: httpcache.ExtensionDirectives{
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
				Extensions: httpcache.ExtensionDirectives{
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
	b.ReportAllocs()

	for b.Loop() {
		_, _ = httpcache.ParseResponseDirectives(`max-age=100, must-revalidate, must-understand, no-cache="Header-1 Header-2", no-store, no-transform, private="Header-3 Header-4", proxy-revalidate, public, s-maxage=200`)
	}
}

func TestParseStructuredResponseDirectives(t *testing.T) {
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
				Extensions: httpcache.ExtensionDirectives{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test")},
				},
			},
		},
		{
			name: `extensions only`,
			in:   `extra, extra-with-value="test"`,
			want: httpcache.ResponseDirectives{
				Extensions: httpcache.ExtensionDirectives{
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
				Extensions: httpcache.ExtensionDirectives{
					{Name: "extra"},
					{Name: "extra-with-value", Value: OptValue("test2")},
				},
			},
		},
		{
			name: `duplicates with same max-/min- values`,
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
				Extensions: httpcache.ExtensionDirectives{
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
				"invalid item",
				"invalid bare item: invalid string: missing closing quote",
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
			got, err := httpcache.ParseStructuredResponseDirectives(tt.in)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseStructuredResponseDirectives() mismatch (-want +got):\n%s", diff)
			}
			if len(tt.wantErr) == 0 && err != nil {
				t.Errorf("ParseStructuredResponseDirectives() error = %v, want nil", err)
			}
			if len(tt.wantErr) > 0 {
				gotErrs := errorStrings(err)

				if diff := cmp.Diff(tt.wantErr, gotErrs); diff != "" {
					t.Errorf("ParseStructuredResponseDirectives() error mismatch (-want +got):\n%s", diff)
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
				Extensions: httpcache.ExtensionDirectives{
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
		privateCache               bool
		date                       time.Time
		expires                    time.Time
		maxAge                     httpcache.Opt[time.Duration]
		sMaxAge                    httpcache.Opt[time.Duration]
		heuristicFreshnessLifetime httpcache.Opt[time.Duration]
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
			name: `no s-maxage, no max-age, no expires, heuristic freshness lifetime`,
			args: args{
				privateCache:               true,
				heuristicFreshnessLifetime: OptValue(30 * time.Second),
			},
			want:   30 * time.Second,
			wantOk: true,
		},
		{
			name: `no s-maxage, no max-age, no expires, shared`,
		},
		{
			name: `no s-maxage, no max-age, no expires, shared, heuristic freshness lifetime`,
			args: args{
				heuristicFreshnessLifetime: OptValue(30 * time.Second),
			},
			want:   30 * time.Second,
			wantOk: true,
		},

		{
			name: `no s-maxage, no max-age, expires`,
			args: args{
				privateCache: true,
				date:         time.Date(2006, time.January, 2, 15, 0o4, 0o5, 0, time.UTC),
				expires:      time.Date(2006, time.January, 2, 15, 0o5, 0o5, 0, time.UTC),
			},
			want:   time.Minute,
			wantOk: true,
		},
		{
			name: `no s-maxage, no max-age, expires, shared`,
			args: args{
				date:    time.Date(2006, time.January, 2, 15, 0o4, 0o5, 0, time.UTC),
				expires: time.Date(2006, time.January, 2, 15, 0o5, 0o5, 0, time.UTC),
			},
			want:   time.Minute,
			wantOk: true,
		},

		{
			name: `no s-maxage, max-age, expires`,
			args: args{
				privateCache: true,
				date:         time.Date(2006, time.January, 2, 15, 0o4, 0o5, 0, time.UTC),
				expires:      time.Date(2006, time.January, 2, 15, 0o5, 0o5, 0, time.UTC),
				maxAge:       OptValue(5 * time.Second),
			},
			want:   5 * time.Second,
			wantOk: true,
		},
		{
			name: `no s-maxage, max-age, expires, shared`,
			args: args{
				date:    time.Date(2006, time.January, 2, 15, 0o4, 0o5, 0, time.UTC),
				expires: time.Date(2006, time.January, 2, 15, 0o5, 0o5, 0, time.UTC),
				maxAge:  OptValue(5 * time.Second),
			},
			want:   5 * time.Second,
			wantOk: true,
		},

		{
			name: `s-maxage, max-age, expires`,
			args: args{
				privateCache: true,
				date:         time.Date(2006, time.January, 2, 15, 0o4, 0o5, 0, time.UTC),
				expires:      time.Date(2006, time.January, 2, 15, 0o5, 0o5, 0, time.UTC),
				maxAge:       OptValue(5 * time.Second),
				sMaxAge:      OptValue(10 * time.Second),
			},
			want:   5 * time.Second,
			wantOk: true,
		},
		{
			name: `s-maxage, max-age, expires, shared`,
			args: args{
				date:    time.Date(2006, time.January, 2, 15, 0o4, 0o5, 0, time.UTC),
				expires: time.Date(2006, time.January, 2, 15, 0o5, 0o5, 0, time.UTC),
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
				tt.args.heuristicFreshnessLifetime,
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
