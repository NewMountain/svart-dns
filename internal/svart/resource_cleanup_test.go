package svart

import (
	"errors"
	"io"
	"testing"
)

func TestRawArchiveReadersPreserveCloseFailures(t *testing.T) {
	failure := errors.New("archive source close failed")
	source := func(closeErr error) rawArchiveSource {
		return func() (*rawArchiveInput, error) {
			return &rawArchiveInput{next: func() ([]rawArchiveRow, error) { return nil, io.EOF }, close: func() error { return closeErr }}, nil
		}
	}
	if err := walkRawArchive(source(failure), func([]rawArchiveRow) error { return nil }); !errors.Is(err, failure) {
		t.Fatalf("walk close failure=%v", err)
	}
	for _, pair := range [][2]error{{failure, nil}, {nil, failure}} {
		if err := equalRawArchiveSources(source(pair[0]), source(pair[1])); !errors.Is(err, failure) {
			t.Errorf("comparison close failure=%v", err)
		}
	}
	visitFailure := errors.New("archive visitor failed")
	rows := func() (*rawArchiveInput, error) {
		return &rawArchiveInput{next: func() ([]rawArchiveRow, error) { return []rawArchiveRow{{Sequence: 1}}, nil }, close: func() error { return failure }}, nil
	}
	if err := walkRawArchive(rows, func([]rawArchiveRow) error { return visitFailure }); !errors.Is(err, failure) || !errors.Is(err, visitFailure) {
		t.Fatalf("combined visitor/close failure=%v", err)
	}
}
