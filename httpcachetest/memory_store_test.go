package httpcachetest_test

import (
	"testing"

	"github.com/nussjustin/httpcache"
	"github.com/nussjustin/httpcache/httpcachetest"
)

func TestMemoryStore(t *testing.T) {
	httpcachetest.TestStore(t, func() httpcache.Store {
		return &httpcachetest.MemoryStore{}
	})
}
