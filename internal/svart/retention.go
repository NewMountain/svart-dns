package svart

import (
	"database/sql"
	"fmt"
	"strconv"
)

// Bound calendar arithmetic to ten thousand years, well within time.Time's
// representation on every supported platform. Larger values are almost
// certainly a unit mistake and must never wrap a deletion cutoff forward.
const maxLogRetentionDays = 3650000

func parseLogRetentionDays(raw string) (int, error) {
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 || days > maxLogRetentionDays {
		return 0, fmt.Errorf("log_retention_days must be an integer between 1 and %d; correct the setting before retention cleanup can run", maxLogRetentionDays)
	}
	return days, nil
}

func readLogRetentionDays(source *sql.DB) (int, error) {
	var raw string
	if err := source.QueryRow("SELECT value FROM settings WHERE key = 'log_retention_days'").Scan(&raw); err != nil {
		return 0, fmt.Errorf("read log_retention_days: %w; restore a valid setting before retention cleanup can run", err)
	}
	return parseLogRetentionDays(raw)
}

func validateSetting(key, value string) error {
	switch key {
	case "log_retention_days":
		_, err := parseLogRetentionDays(value)
		return err
	case "sync_interval":
		_, err := parseSyncInterval(value)
		return err
	}
	return nil
}
