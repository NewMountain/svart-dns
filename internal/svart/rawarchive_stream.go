package svart

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/parquet-go/parquet-go"
)

const rawArchiveSegmentRows = 65536
const rawArchiveSegmentBytes = 64 << 20

type rawArchiveDigest struct {
	hash                             hash.Hash
	count                            int
	first, last, minNS, maxNS, bytes int64
}

func (d *rawArchiveDigest) add(rows []rawArchiveRow) {
	if d.hash == nil {
		d.hash = sha256.New()
	}
	var b [8]byte
	for _, r := range rows {
		if d.count == 0 {
			d.first = r.Sequence
			d.minNS = r.TimestampNS
			d.maxNS = r.TimestampNS
		}
		if r.TimestampNS < d.minNS {
			d.minNS = r.TimestampNS
		}
		if r.TimestampNS > d.maxNS {
			d.maxNS = r.TimestampNS
		}
		d.last = r.Sequence
		d.count++
		d.bytes += int64(len(r.Payload))
		for _, v := range []int64{int64(len(r.Journal)), r.Sequence, r.TimestampNS, int64(len(r.Payload))} {
			// #nosec G115 -- Canonical digest encodes signed timestamps as their exact two's-complement 64-bit wire representation.
			binary.LittleEndian.PutUint64(b[:], uint64(v))
			d.hash.Write(b[:])
		}
		d.hash.Write([]byte(r.Journal))
		d.hash.Write(r.Payload)
	}
}
func (d *rawArchiveDigest) manifest(partition string) rawArchiveManifest {
	digest := hex.EncodeToString(d.hash.Sum(nil))
	return rawArchiveManifest{name: filepath.Join(partition, fmt.Sprintf("%020d-%020d-%s.parquet", d.first, d.last, digest)), first: d.first, last: d.last, count: d.count, digest: digest, state: "pending", minNS: d.minNS, maxNS: d.maxNS}
}
func (d *rawArchiveDigest) verify(m rawArchiveManifest) error {
	if d.count != m.count {
		return fmt.Errorf("raw archive %s count=%d expected=%d; source preserved", m.name, d.count, m.count)
	}
	got := d.manifest("")
	if got.first != m.first || got.last != m.last || got.digest != m.digest || got.minNS != m.minNS || got.maxNS != m.maxNS {
		return fmt.Errorf("raw archive %s content mismatch; source preserved", m.name)
	}
	return nil
}

// A source creates a fresh bounded iterator. Factories allow independent source
// verification, writing and byte-for-byte read-back without holding a segment.
type rawArchiveInput struct {
	next  func() ([]rawArchiveRow, error)
	close func() error
}
type rawArchiveSource func() (*rawArchiveInput, error)

func walkRawArchive(source rawArchiveSource, visit func([]rawArchiveRow) error) (resultErr error) {
	input, err := source()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, input.close()) }()
	for {
		rows, err := input.next()
		if len(rows) > 0 {
			if e := visit(rows); e != nil {
				return e
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
func (a *rawArchiver) journalSource(first, last int64) rawArchiveSource {
	return func() (*rawArchiveInput, error) {
		after := first - 1
		return &rawArchiveInput{close: func() error { return nil }, next: func() ([]rawArchiveRow, error) {
			rows, err := a.originalRows(after+1, last)
			if err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				return nil, io.EOF
			}
			after = rows[len(rows)-1].Sequence
			return rows, nil
		}}, nil
	}
}
func (a *rawArchiver) fileSource(m rawArchiveManifest) rawArchiveSource {
	return func() (*rawArchiveInput, error) {
		// #nosec G304 G703 -- Archive root is operator selected; names are generated or validated by scanRawManifest, and directory walks use actual entry names.
		f, err := os.Open(filepath.Join(a.dir, m.name))
		if err != nil {
			return nil, err
		}
		fail := func(err error) (*rawArchiveInput, error) { return nil, errors.Join(err, f.Close()) }
		info, err := f.Stat()
		if err != nil {
			return fail(err)
		}
		pf, err := parquet.OpenFile(f, info.Size())
		if err != nil {
			return fail(err)
		}
		if !parquet.EqualNodes(pf.Schema(), parquet.SchemaOf(rawArchiveRow{})) {
			return fail(errors.New("raw archive schema mismatch; preserve sources"))
		}
		if pf.NumRows() != int64(m.count) {
			return fail(fmt.Errorf("raw archive %s footer count mismatch", m.name))
		}
		reader := parquet.NewGenericReader[rawArchiveRow](pf)
		var digest rawArchiveDigest
		var previous int64
		return &rawArchiveInput{close: func() error { return errors.Join(reader.Close(), f.Close()) }, next: func() ([]rawArchiveRow, error) {
			rows := make([]rawArchiveRow, rawArchiveBatchRows)
			n, size := 0, 0
			var err error
			for n < len(rows) && size < rawArchiveBatchBytes {
				var read int
				read, err = reader.Read(rows[n : n+1])
				if read > 0 {
					size += len(rows[n].Payload)
					n += read
				}
				if err != nil {
					break
				}
			}
			rows = rows[:n]
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			for _, r := range rows {
				ts, e := rawTimestamp(r.Payload)
				if e != nil {
					return nil, e
				}
				if r.Journal != a.journal.identity || r.Sequence <= previous || r.TimestampNS != ts {
					return nil, errors.New("raw archive identity/order/timestamp mismatch")
				}
				previous = r.Sequence
			}
			digest.add(rows)
			if errors.Is(err, io.EOF) {
				if e := digest.verify(m); e != nil {
					return nil, e
				}
			}
			return rows, err
		}}, nil
	}
}
func filterRawArchive(source rawArchiveSource, cutoff int64) rawArchiveSource {
	return func() (*rawArchiveInput, error) {
		input, err := source()
		if err != nil {
			return nil, err
		}
		return &rawArchiveInput{close: input.close, next: func() ([]rawArchiveRow, error) {
			for {
				rows, err := input.next()
				kept := make([]rawArchiveRow, 0, len(rows))
				for _, r := range rows {
					if r.TimestampNS >= cutoff {
						kept = append(kept, r)
					}
				}
				if len(kept) > 0 || err != nil {
					return kept, err
				}
			}
		}}, nil
	}
}
func digestRawArchive(source rawArchiveSource) (rawArchiveDigest, error) {
	var digest rawArchiveDigest
	err := walkRawArchive(source, func(rows []rawArchiveRow) error { digest.add(rows); return nil })
	return digest, err
}
func equalRawArchiveSources(want, got rawArchiveSource) (resultErr error) {
	left, err := want()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, left.close()) }()
	right, err := got()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, right.close()) }()
	type cursor struct {
		input *rawArchiveInput
		rows  []rawArchiveRow
		err   error
	}
	pop := func(c *cursor) (rawArchiveRow, error) {
		for len(c.rows) == 0 && c.err == nil {
			c.rows, c.err = c.input.next()
		}
		if len(c.rows) == 0 {
			return rawArchiveRow{}, c.err
		}
		r := c.rows[0]
		c.rows = c.rows[1:]
		return r, nil
	}
	l, r := cursor{input: left}, cursor{input: right}
	for {
		lr, le := pop(&l)
		rr, re := pop(&r)
		if le != nil || re != nil {
			if errors.Is(le, io.EOF) && errors.Is(re, io.EOF) {
				return nil
			}
			return fmt.Errorf("raw archive equality read failed: source=%v archive=%v", le, re)
		}
		if err = equalRawRows([]rawArchiveRow{lr}, []rawArchiveRow{rr}); err != nil {
			return err
		}
	}
}
