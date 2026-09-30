package main

import (
	"net/url"
	"path/filepath"
)

// sqliteFileDSN keeps filesystem bytes separate from SQLite URI options. A
// literal '#', '?' or percent escape in an operator-selected path must never
// choose a different database for another connection pool.
func sqliteFileDSN(path, options string) string {
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: options}
	return uri.String()
}
