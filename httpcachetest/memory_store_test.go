package httpcachetest_test

import (
	"testing"
	"time"

	"github.com/nussjustin/httpcache"
	"github.com/nussjustin/httpcache/httpcachetest"
)

func TestMemoryStore(t *testing.T) {
	t.Run("Default", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{},
			func() httpcache.Store {
				return httpcachetest.NewMemoryStore(httpcachetest.MemoryStoreOpts{})
			},
		)
	})

	t.Run("ReturnsNilForExpiredResponses", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{
				ReturnsNilForExpiredResponses: true,
			},
			func() httpcache.Store {
				return httpcachetest.NewMemoryStore(httpcachetest.MemoryStoreOpts{
					ReturnNilForExpiredResponses: true,
				})
			},
		)
	})

	t.Run("ReturnsNilForExpiredResponsesAfter", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{
				ReturnsNilForExpiredResponses:      true,
				ReturnsNilForExpiredResponsesAfter: 30 * time.Second,
			},
			func() httpcache.Store {
				return httpcachetest.NewMemoryStore(httpcachetest.MemoryStoreOpts{
					ReturnNilForExpiredResponses:      true,
					ReturnNilForExpiredResponsesAfter: 30 * time.Second,
				})
			},
		)
	})

	t.Run("ValidatesNoVarySearch", func(t *testing.T) {
		httpcachetest.TestStore(t,
			httpcachetest.TestStoreOpts{
				ValidatesNoVarySearch: true,
			},
			func() httpcache.Store {
				return httpcachetest.NewMemoryStore(httpcachetest.MemoryStoreOpts{
					ValidateNoVarySearch: true,
				})
			},
		)
	})
}
