# httpcache [![Go Reference](https://pkg.go.dev/badge/github.com/nussjustin/httpcache.svg)](https://pkg.go.dev/github.com/nussjustin/httpcache) [![Lint](https://github.com/nussjustin/httpcache/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/nussjustin/httpcache/actions/workflows/golangci-lint.yml) [![Test](https://github.com/nussjustin/httpcache/actions/workflows/test.yml/badge.svg)](https://github.com/nussjustin/httpcache/actions/workflows/test.yml)

Package httpcache implements functions related to HTTP caching based on RFC 9111.

It also implements

* Targeted Cache-Control headers (RFC 9213),
* The Cache-Groups and Cache-Group-Invalidation headers (RFC 9875),
* The Cache-Status header (RFC 9211) and
* The No-Vary-Search header ([IETF HTTP Working Group draft](https://github.com/httpwg/http-extensions/blob/14b1143b9fe8cb4ff31257aaf44fc8dcb2314bed/draft-ietf-httpbis-no-vary-search.md)

## Examples

### Using a client

Note: The full code for this example can be found in `examples/using-a-client/main.go`.

The `httpcache.Client` type wraps an existing HTTP client (defaulting to `net/http.DefaultClient`) and adds caching for
requests.

It supports both serving responses directly from the cache without making a request as well as validating stale requests
based on the `ETag`/`If-None-Match` and `Last-Modified`/`If-Modified-Since` headers.

It can also add the `Cache-Status` header specified in RFC 9213 to responses.

To use a `httpcache.Client` first create the struct and give it a `httpcache.Config`, which defines how the cache should
behave, and a `httpcache.Store`, which does the actual storing of responses and retrieval of cached responses.

```go
client := httpcache.Client{
    Config: httpcache.Config{
        Name:           "my-awesome-app",
        AddCacheStatus: true,
    },
    Store: memstore.New(memstore.Options{}),
}
```

In this example we simply configure the cache with the name "my-awesome-app" and enable the `Cache-Status` header (the
name is only necessary for the `Cache-Status` header).

We also configure an in-memory cache store.

Now we can make a request:

```go
req1, err := http.NewRequestWithContext(ctx, "GET", "https://example.com/", nil)
if err != nil {
    panic(err)
}

resp1, err := client.Do(req1)
if err != nil {
    panic(err)
}
drainBody(resp1)

fmt.Println(resp1.Status)
// Result: 200 OK
```

Since the store is still empty the request will be sent to the server and the response stored in the cache store. We
can check the `Cache-Status` header to see that the response was stored: 

```go
fmt.Println(resp1.Header.Get("Cache-Status"))
// Result: "my-awesome-app";fwd=miss;stored
```

Here `fwd=miss` part means "the request was forwarded because it was not found in the cache". `stored` indicates that
the response was indeed stored for the future.

Now make another request for the same URL:

```go
req2, err := http.NewRequestWithContext(ctx, "GET", "https://example.com/", nil)
if err != nil {
    panic(err)
}
req2.Header.Set("Cache-Control", "public, max-stale=60")

resp2, err := client.Do(req2)
if err != nil {
    panic(err)
}
drainBody(resp2)

fmt.Println(resp2.Status)
// Result: 200 OK
```

This time we add a `Cache-Control` header to the request. The header defines a maximum time of 60 seconds during which
a response can be considered stale and still returned, as long as it can be validated. Without this the client would be
unable to reuse the cached response.

Let's check the `Cache-Status` header:

```go
fmt.Println(resp2.Header.Get("Cache-Status"))
// Result: "my-awesome-app";fwd=stale;fwd-status=304;ttl=0
```

`fwd=stale` means that the request was considered stale, in other words it could not be used to completely skip the
request to the server, but allowed the client to make a "conditional" request, that is a request containing `ETag`
and `Last-Modified` headers.

`fwd-status=304` defines the HTTP status code that was returned for the request. `304` means "Not Modified", in other
words the server confirmed that our cached response was still valid and skipped calculating its own response.

`ttl=0` indicates the responses remaining "freshness lifetime", in other words the number of seconds for which the
response can still be considered fresh.

## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

Please make sure to update tests as appropriate.

## License
[MIT](https://choosealicense.com/licenses/mit/)