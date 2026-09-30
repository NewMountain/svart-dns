package main

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// SystemSample holds a single point-in-time system resource snapshot.
type SystemSample struct {
	Timestamp   time.Time `json:"timestamp"`
	CPUPercent  float64   `json:"cpu_percent"`
	RSSBytes    uint64    `json:"rss_bytes"`
	HeapAlloc   uint64    `json:"heap_alloc"`
	DBSizeBytes int64     `json:"db_size_bytes"`
}

const systemStatsBufferSize = 120

var (
	systemStatsMu      sync.RWMutex
	systemStatsBuffer  [systemStatsBufferSize]SystemSample
	systemStatsHead    int  // next write position
	systemStatsCount   int  // samples collected so far (up to bufferSize)
	systemStatsStarted bool // guard against double-start

	// Previous Getrusage + wall-clock for CPU % diff
	lastRusageUser int64 // microseconds
	lastRusageSys  int64 // microseconds
	lastSampleTime time.Time
)

// startSystemStatsCollector launches the background goroutine that samples
// system stats every 30 seconds into the ring buffer (120 samples = 60 min history).
func startSystemStatsCollector() {
	systemStatsMu.Lock()
	if systemStatsStarted {
		systemStatsMu.Unlock()
		return
	}
	systemStatsStarted = true

	lastSampleTime = time.Time{}
	systemStatsMu.Unlock()

	// Immediate first sample (0% CPU is fine)
	collectSystemSample()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			collectSystemSample()
		}
	}()
}

// collectSystemSample gathers CPU, memory, and disk stats and writes to the ring buffer.
func collectSystemSample() {
	collectSystemSampleWithUsage(func(ru *syscall.Rusage) error { return syscall.Getrusage(syscall.RUSAGE_SELF, ru) })
}

func collectSystemSampleWithUsage(readUsage func(*syscall.Rusage) error) {
	now := time.Now()
	numCPU := float64(runtime.NumCPU())

	// --- CPU % via Getrusage diff ---
	var ru syscall.Rusage
	if err := readUsage(&ru); err != nil {
		slog.Error("system resource sample unavailable", "component", "system", "error", err)
		return
	}
	systemStatsMu.Lock()
	defer systemStatsMu.Unlock()
	curUser := ru.Utime.Sec*1e6 + int64(ru.Utime.Usec)
	curSys := ru.Stime.Sec*1e6 + int64(ru.Stime.Usec)

	elapsed := now.Sub(lastSampleTime)
	var cpuPct float64
	if !lastSampleTime.IsZero() && elapsed > 0 {
		cpuUsed := float64((curUser - lastRusageUser) + (curSys - lastRusageSys)) // microseconds
		wallUs := elapsed.Seconds() * 1e6
		cpuPct = (cpuUsed / wallUs) * 100.0 / numCPU
		if cpuPct < 0 {
			cpuPct = 0
		}
		if cpuPct > 100 {
			cpuPct = 100
		}
	}
	lastRusageUser = curUser
	lastRusageSys = curSys
	lastSampleTime = now

	// --- Memory ---
	rssBytes := readRSSBytes()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	// --- Disk: DB file sizes ---
	dbSize := dbFileSize(dbPathGlobal)

	sample := SystemSample{
		Timestamp:   now,
		CPUPercent:  cpuPct,
		RSSBytes:    rssBytes,
		HeapAlloc:   mem.Alloc,
		DBSizeBytes: dbSize,
	}

	systemStatsBuffer[systemStatsHead] = sample
	systemStatsHead = (systemStatsHead + 1) % systemStatsBufferSize
	if systemStatsCount < systemStatsBufferSize {
		systemStatsCount++
	}
}

// readRSSBytes reads the process RSS from /proc/self/statm.
// Returns 0 on any error (non-Linux).
func readRSSBytes() uint64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	return rssBytesFromStatm(data, os.Getpagesize())
}

func rssBytesFromStatm(data []byte, pageSize int) uint64 {
	if pageSize <= 0 {
		return 0
	}
	var size, resident uint64
	if _, err := fmt.Sscanf(string(data), "%d %d", &size, &resident); err != nil {
		return 0
	}
	if resident > math.MaxUint64/uint64(pageSize) {
		return 0
	}
	return resident * uint64(pageSize)
}

// dbFileSize returns the combined size of the main DB, WAL, and SHM files.
func dbFileSize(path string) int64 {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, err := os.Stat(path + suffix); err == nil {
			total += info.Size()
		}
	}
	return total
}

// getSystemSamples returns the ring buffer contents in chronological order.
func getSystemSamples() []SystemSample {
	systemStatsMu.RLock()
	defer systemStatsMu.RUnlock()

	if systemStatsCount == 0 {
		return []SystemSample{}
	}

	result := make([]SystemSample, systemStatsCount)
	if systemStatsCount < systemStatsBufferSize {
		// Buffer not yet full — data starts at index 0
		copy(result, systemStatsBuffer[:systemStatsCount])
	} else {
		// Buffer full — oldest is at systemStatsHead
		start := systemStatsHead
		n := copy(result, systemStatsBuffer[start:])
		copy(result[n:], systemStatsBuffer[:start])
	}
	return result
}

// getLatestSystemSample returns the most recent sample, or a zero sample if none collected.
func getLatestSystemSample() SystemSample {
	systemStatsMu.RLock()
	defer systemStatsMu.RUnlock()
	if systemStatsCount == 0 {
		return SystemSample{}
	}
	idx := (systemStatsHead - 1 + systemStatsBufferSize) % systemStatsBufferSize
	return systemStatsBuffer[idx]
}

// handleAPIStatsSystem returns the system stats ring buffer as JSON.
// @Summary Get system resource stats
// @Tags stats
// @Produce json
// @Success 200 {object} apiResponse{data=[]SystemSample}
// @Router /api/stats/system [get]
func handleAPIStatsSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, getSystemSamples())
}
