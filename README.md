# httpcache [![Go Reference](https://pkg.go.dev/badge/github.com/nussjustin/httpcache.svg)](https://pkg.go.dev/github.com/nussjustin/httpcache) [![Lint](https://github.com/nussjustin/httpcache/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/nussjustin/httpcache/actions/workflows/golangci-lint.yml) [![Test](https://github.com/nussjustin/httpcache/actions/workflows/test.yml/badge.svg)](https://github.com/nussjustin/httpcache/actions/workflows/test.yml)

Package httpcache implements functions related to HTTP caching based on RFC 9111 including support for the
Cache-Status header specified in RFC 9211 and the No-Vary-Search header¹ specified in
draft-ietf-httpbis-no-vary-search-10.

¹ Note: The No-Vary-Search header is currently not used when caching.

## Contributing
Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

Please make sure to update tests as appropriate.

## License
[MIT](https://choosealicense.com/licenses/mit/)