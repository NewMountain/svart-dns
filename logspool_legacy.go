package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type legacyProvenance struct {
	Size, Cursor int64
	Digest       string
}

func legacyPrefixDigest(f *os.File, size int64) (string, error) {
	hash := sha256.New()
	n, err := io.Copy(hash, io.NewSectionReader(f, 0, size))
	if err != nil {
		return "", err
	}
	if n != size {
		return "", errors.New("legacy source changed during prefix verification; retain original")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// The old binary can append again after rollback. The marker alone is not
// ownership: compare the complete previously observed prefix before admitting
// a new tail. Replaced/truncated/reused files require explicit reconciliation.
func (s *logSpool) importLegacy() error {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer closeReadResource(f)
	info, err := f.Stat()
	if err != nil {
		return err
	}
	var previous legacyProvenance
	var encoded string
	err = s.journal.db.QueryRow("SELECT value FROM journal_meta WHERE key='legacy_provenance_v1'").Scan(&encoded)
	if err == nil {
		if err := json.Unmarshal([]byte(encoded), &previous); err != nil {
			return err
		}
		if previous.Cursor < 0 || previous.Cursor > previous.Size || previous.Size > info.Size() {
			return errors.New("legacy source was truncated or replaced after import; preserve file and reconcile rollback originals")
		}
		actual, err := legacyPrefixDigest(f, previous.Size)
		if err != nil {
			return err
		}
		if actual != previous.Digest {
			return errors.New("legacy source prefix changed after import; preserve file and reconcile rollback originals")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	} else {
		var marked bool
		if err := s.journal.db.QueryRow("SELECT EXISTS(SELECT 1 FROM journal_meta WHERE key='legacy_imported')").Scan(&marked); err != nil {
			return err
		}
		if marked {
			return errors.New("legacy import has no exact prefix provenance; preserve files and reconcile before startup")
		}
		if raw, err := os.ReadFile(s.offsetPath); err == nil {
			previous.Cursor, err = strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
			if err != nil || previous.Cursor < 0 {
				return fmt.Errorf("invalid legacy journal offset in %s", s.offsetPath)
			}
			if info.Size() == 0 {
				previous.Cursor = 0
			} else if previous.Cursor > info.Size() {
				return fmt.Errorf("legacy journal offset exceeds file size: %s", s.path)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	digest, err := legacyPrefixDigest(f, info.Size())
	if err != nil {
		return err
	}
	tx, err := s.journal.db.Begin()
	if err != nil {
		return err
	}
	defer rollbackTransaction(tx)
	reader := bufio.NewReader(io.NewSectionReader(f, previous.Cursor, info.Size()-previous.Cursor))
	position, cursor := previous.Cursor, previous.Cursor
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if line[len(line)-1] != '\n' {
				// Every observed incomplete version has separate provenance, so a later
				// completed append cannot overwrite a quarantined original fragment.
				source := s.path + "#sha256=" + digest
				result, err := tx.Exec("INSERT OR IGNORE INTO journal_quarantine(source,position,payload,reason) VALUES(?,?,?,?)", source, position, line, "incomplete legacy append; original file preserved")
				if err != nil {
					return err
				}
				n, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if n != 1 {
					var stored []byte
					if err := tx.QueryRow("SELECT payload FROM journal_quarantine WHERE source=? AND position=?", source, position).Scan(&stored); err != nil {
						return err
					}
					if !bytes.Equal(stored, line) {
						return errors.New("legacy quarantine identity conflicts with original bytes")
					}
				}
			} else {
				var record spoolRecord
				if err := json.Unmarshal(line, &record); err != nil {
					return fmt.Errorf("legacy journal corrupt at byte %d in %s: %w", position, s.path, err)
				}
				token := fmt.Sprintf("legacy:%d", position)
				result, err := tx.Exec("INSERT OR IGNORE INTO journal_records(token,payload) VALUES(?,?)", token, line)
				if err != nil {
					return err
				}
				n, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if n != 1 {
					var stored []byte
					if err := tx.QueryRow("SELECT payload FROM journal_records WHERE token=?", token).Scan(&stored); err != nil {
						return err
					}
					if !bytes.Equal(stored, line) {
						return errors.New("legacy retry identity conflicts with original bytes; preserve source and reconcile")
					}
				}
				cursor = position + int64(len(line))
			}
			position += int64(len(line))
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	// Reject an in-place rewrite during import; an append beyond the captured
	// file size remains a new tail for the next startup.
	actual, err := legacyPrefixDigest(f, info.Size())
	if err != nil {
		return err
	}
	if actual != digest {
		return errors.New("legacy source changed during import; preserve original and retry")
	}
	encodedBytes, err := json.Marshal(legacyProvenance{info.Size(), cursor, digest})
	if err != nil {
		return err
	}
	if err := rawArchiveChange(tx, "INSERT INTO journal_meta(key,value) VALUES('legacy_provenance_v1',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(encodedBytes)); err != nil {
		return err
	}
	if err := rawArchiveChange(tx, "INSERT INTO journal_meta(key,value) VALUES('legacy_imported','true') ON CONFLICT(key) DO UPDATE SET value=excluded.value"); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return tx.Commit()
}
