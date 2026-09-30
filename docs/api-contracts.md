# API wire types

HTTP success values use a concrete `APIResponse[T]` envelope (`data` and a null
`error`). Error responses keep the compatible string `error` and add the stable
`error_code` documented in [API errors](api-errors.md). Handler request and response
records live in `api_requests.go` and `api_dto.go`; domain records are reused where
the existing wire shape already matches them.

Optional response fields use pointers so an explicit false, zero, or empty string
remains present when the original handler included it. Empty collections keep their
existing array representation. Investigation rows are JSON values because SQL
expressions determine their value types; they are encoded once at the HTTP boundary.

Archive status reads both live storage and archive metadata. A nonexistent archive
directory is the valid state before the first archive. Other directory errors,
metadata failures, invalid stored timestamps, and database failures return 503 with
no partial success payload. JSON bodies must be a non-null value of the expected
type and contain exactly one JSON document.
