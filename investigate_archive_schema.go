package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/parquet-go/parquet-go"
)

var errInvestigateArchiveSchema = errors.New("invalid archive schema")

// Group equal physical schemas before asking DuckDB to bind the archives.
// union_by_name eagerly retains every file's metadata, which exhausts the
// worker budget on ordinary multi-million-row histories. Each homogeneous
// scan can load files lazily; UNION ALL BY NAME between schema groups retains
// compatibility with old archives that lack newer columns.
func investigateArchiveScans(ctx context.Context, archiveDir string) ([]string, error) {
	entries, err := os.ReadDir(archiveDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	groups := make([][]string, 0)
	indexes := make(map[string]int)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".parquet" {
			continue
		}
		path := filepath.Join(archiveDir, entry.Name())
		schema, err := investigateArchiveSchema(path)
		if err != nil {
			return nil, fmt.Errorf("read archive schema: %w", err)
		}
		index, ok := indexes[schema]
		if !ok {
			index = len(groups)
			indexes[schema] = index
			groups = append(groups, nil)
		}
		// DuckDB expands glob syntax even in an explicit file list. Quote literal
		// metacharacters so an operator's filename cannot duplicate another file.
		literal := strings.NewReplacer("[", "[[]", "*", "[*]", "?", "[?]").Replace(path)
		groups[index] = append(groups[index], sqlStringLiteral(literal))
	}
	scans := make([]string, 0, len(groups))
	for _, paths := range groups {
		scans = append(scans, `SELECT * REPLACE (epoch_ms("timestamp") AS "timestamp") FROM read_parquet([`+strings.Join(paths, ",")+`], union_by_name = false)`)
	}
	return scans, nil
}

func investigateArchiveSchema(path string) (schema string, err error) {
	// #nosec G304 G703 -- Paths come from directory entries under the configured archive directory, never from query text.
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	metadata, err := parquet.OpenFile(file, info.Size(), parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true))
	if err != nil {
		return "", fmt.Errorf("%w: %w", errInvestigateArchiveSchema, err)
	}
	encoded, err := json.Marshal(metadata.Metadata().Schema)
	return string(encoded), err
}
