package httpcache

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"strings"
)

type bytesOrStringsReader interface {
	*bytes.Reader | *strings.Reader

	io.Reader
	io.Seeker

	Len() int
}

func readBody(rc *io.ReadCloser, size int) (body []byte, err error) {
	if *rc == nil {
		return nil, nil
	}

	r := unwrapReader(*rc)

	switch x := r.(type) {
	case *bytes.Buffer:
		if x.Len() == 0 {
			return nil, nil
		}

		return slices.Clone(x.Bytes()), nil
	case *bytes.Reader:
		return readBodyFromBytesOrStringsReader(x), nil
	case *strings.Reader:
		return readBodyFromBytesOrStringsReader(x), nil
	}

	if size > 0 && size < math.MaxInt {
		body, err = readAllN(r, size)
	} else {
		body, err = io.ReadAll(r)
	}
	if err != nil {
		return nil, err
	}

	*rc = io.NopCloser(bytes.NewReader(body))

	return body, nil
}

func readBodyFromBytesOrStringsReader[T bytesOrStringsReader](r T) []byte {
	if r.Len() == 0 {
		return nil
	}

	body := make([]byte, r.Len())

	if _, err := io.ReadFull(r, body); err != nil {
		panic("unreachable")
	}

	if _, err := r.Seek(-int64(len(body)), io.SeekCurrent); err != nil {
		panic("unreachable")
	}

	return body
}

func readAllN(r io.Reader, rn int) ([]byte, error) {
	buf := make([]byte, rn+1)

	var n int
	var err error

	for n < rn && err == nil {
		var nn int
		nn, err = r.Read(buf[n:])
		n += nn
	}

	if err == io.EOF {
		err = nil
	}

	if err != nil {
		return nil, err
	}

	if n < rn {
		return nil, fmt.Errorf("read too few bytes: got %d, want %d", n, rn)
	}

	if n > rn+1 {
		return nil, fmt.Errorf("read too many bytes: got %d, want %d", n, rn)
	}

	return buf, nil
}

func unwrapReader(rc io.ReadCloser) io.Reader {
	var r io.Reader = rc

	for {
		v := reflect.ValueOf(r)

		t := v.Type()

		if t.PkgPath() != "io" {
			return r
		}

		if n := t.Name(); n != "nopCloser" && n != "nopCloserWriterTo" {
			return r
		}

		r = v.Field(0).Interface().(io.Reader)
	}
}
