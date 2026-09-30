package svart

import (
	"math"
	"strconv"
	"strings"
	"sync/atomic"
)

type runtimeSettingsSnapshot struct {
	strategy     string
	cacheTTL     int
	bootstrapTTL int
	deniedTTL    uint32
}

var runtimeSettings atomic.Value // runtimeSettingsSnapshot

func init() {
	runtimeSettings.Store(defaultRuntimeSettingsSnapshot())
}

func defaultRuntimeSettingsSnapshot() runtimeSettingsSnapshot {
	return runtimeSettingsSnapshot{
		strategy:     "weighted",
		cacheTTL:     3600,
		bootstrapTTL: 3600,
		deniedTTL:    3600,
	}
}

func isRuntimeSettingKey(key string) bool {
	switch key {
	case "strategy", "cache_ttl", "bootstrap_ttl", "denied_ttl":
		return true
	default:
		return false
	}
}

func parseRuntimeIntSetting(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

func parseDeniedTTLValue(value string) uint32 {
	ttl, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || ttl <= 0 || ttl > math.MaxUint32 {
		return 3600
	}
	return uint32(ttl)
}

func readRuntimeSettings(q queryer) (runtimeSettingsSnapshot, error) {
	snapshot := runtimeSettingsSnapshot{deniedTTL: 3600}
	seen := make(map[string]bool, 4)

	rows, err := q.Query(`
		SELECT key, value
		FROM settings
		WHERE key IN ('strategy', 'cache_ttl', 'bootstrap_ttl', 'denied_ttl')
	`)
	if err != nil {
		return snapshot, err
	}
	defer closeQueryRows(rows)

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return snapshot, err
		}
		seen[key] = true

		switch key {
		case "strategy":
			snapshot.strategy = strings.TrimSpace(value)
		case "cache_ttl":
			snapshot.cacheTTL = parseRuntimeIntSetting(value)
		case "bootstrap_ttl":
			snapshot.bootstrapTTL = parseRuntimeIntSetting(value)
		case "denied_ttl":
			snapshot.deniedTTL = parseDeniedTTLValue(value)
		}
	}
	if err := rows.Err(); err != nil {
		return snapshot, err
	}

	defaults := defaultRuntimeSettingsSnapshot()
	if !seen["strategy"] {
		snapshot.strategy = defaults.strategy
	}
	if !seen["cache_ttl"] {
		snapshot.cacheTTL = defaults.cacheTTL
	}
	if !seen["bootstrap_ttl"] {
		snapshot.bootstrapTTL = defaults.bootstrapTTL
	}
	if !seen["denied_ttl"] {
		snapshot.deniedTTL = defaults.deniedTTL
	}

	return snapshot, nil
}

func loadRuntimeSettingsFromDB() error {
	snapshot, err := readRuntimeSettings(db)
	if err != nil {
		return err
	}
	runtimeSettings.Store(snapshot)
	return nil
}

func getRuntimeSettings() runtimeSettingsSnapshot {
	snapshot, ok := runtimeSettings.Load().(runtimeSettingsSnapshot)
	if !ok {
		panic("runtime settings snapshot has an invalid internal type")
	}
	return snapshot
}

func getResolutionStrategy() string {
	return getRuntimeSettings().strategy
}

func getCacheTTL() int {
	return getRuntimeSettings().cacheTTL
}

func getBootstrapTTL() int {
	return getRuntimeSettings().bootstrapTTL
}

func getDeniedTTL() uint32 {
	return getRuntimeSettings().deniedTTL
}
