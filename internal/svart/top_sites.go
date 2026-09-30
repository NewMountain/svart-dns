package svart

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

const maxTopSitesBytes = 64 << 20

func getTopSites(path string, limit int) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("top-sites unavailable: TOP_SITES_PATH is unset; configure a local rank,domain CSV file")
	}
	// TOP_SITES_PATH is operator configuration; requests cannot choose a path.
	info, err := os.Stat(path) // #nosec G703 -- local operator-selected CSV paths are intentionally supported.
	if err != nil {
		return nil, fmt.Errorf("top-sites unavailable: cannot access TOP_SITES_PATH; check the file exists and is readable")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("top-sites unavailable: TOP_SITES_PATH must be a regular CSV file")
	}
	if info.Size() > maxTopSitesBytes {
		return nil, fmt.Errorf("top-sites unavailable: TOP_SITES_PATH exceeds the %d-byte source budget; provide a smaller complete ranking", maxTopSitesBytes)
	}
	f, err := os.Open(path) // #nosec G304 G703 -- same operator-selected local CSV verified above, not request input.
	if err != nil {
		return nil, fmt.Errorf("top-sites unavailable: cannot open TOP_SITES_PATH; check file permissions")
	}
	defer func() {
		if err := f.Close(); err != nil {
			logAdmin.Error("close configured top-sites CSV", "error", err)
		}
	}()
	domains, err := parseTopSites(f, limit)
	if err != nil {
		return nil, fmt.Errorf("top-sites unavailable: fix TOP_SITES_PATH: %w", err)
	}
	return domains, nil
}

// A response limit selects a prefix only after the entire source is validated.
// Source budgets reject the file outright, never accept a truncated ranking.
func parseTopSites(r io.Reader, limit int) ([]string, error) {
	bounded := &io.LimitedReader{R: r, N: maxTopSitesBytes + 1}
	scanner := bufio.NewScanner(bounded)
	scanner.Buffer(make([]byte, 1024), 1024)
	domains := make([]string, 0, limit)
	var previous uint64
	row := 0
	for scanner.Scan() {
		row++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		reader := csv.NewReader(strings.NewReader(line))
		reader.FieldsPerRecord = -1
		record, err := reader.Read()
		if err != nil {
			return nil, fmt.Errorf("row %d: invalid CSV; use one rank,domain record per line", row)
		}
		if len(record) != 2 {
			return nil, fmt.Errorf("row %d: expected rank,domain", row)
		}
		rank, err := strconv.ParseUint(record[0], 10, 64)
		if err != nil || rank == 0 {
			return nil, fmt.Errorf("row %d: rank must be a positive integer", row)
		}
		if rank <= previous {
			return nil, fmt.Errorf("row %d: ranks must be strictly increasing", row)
		}
		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(record[1]), "."))
		if !validTopSiteDomain(domain) {
			return nil, fmt.Errorf("row %d: invalid domain; use an ASCII hostname (punycode for international names)", row)
		}
		previous = rank
		if len(domains) < limit {
			domains = append(domains, domain)
		}
	}
	if bounded.N == 0 {
		return nil, fmt.Errorf("source exceeds the %d-byte budget; provide a smaller complete ranking", maxTopSitesBytes)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("cannot read complete CSV at row %d; check storage and keep each record below 1024 bytes", row+1)
	}
	if previous == 0 {
		return nil, fmt.Errorf("CSV contains no domains; provide rank,domain records without a header")
	}
	return domains, nil
}

func validTopSiteDomain(domain string) bool {
	if len(domain) > maxDNSNameLength || net.ParseIP(domain) != nil {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
				return false
			}
		}
	}
	return true
}
