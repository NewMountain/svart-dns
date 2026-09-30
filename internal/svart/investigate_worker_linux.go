//go:build linux

package svart

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// Linux >= 4.7 applies RLIMIT_DATA to private writable mmap as well as brk.
// This includes DuckDB mallocs AND the Go heap/serialization copies. Unlike
// RLIMIT_RSS, allocation refusal is synchronous, not sampled after allocation.
const investigateWorkerDataBytes = 384 << 20

func limitInvestigateWorker() error {
	// Fail closed on old kernels whose DATA limit does not constrain mmap.
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err != nil {
		return err
	}
	var release []byte
	for _, c := range uts.Release {
		if c == 0 {
			break
		}
		// Utsname uses signed bytes on this architecture; preserve their raw bit pattern.
		release = append(release, byte(int16(c)&0xff))
	}
	parts := strings.Split(string(release), ".")
	if len(parts) < 2 {
		return fmt.Errorf("unknown kernel")
	}
	major, e1 := strconv.Atoi(parts[0])
	minor, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || major < 4 || major == 4 && minor < 7 {
		return fmt.Errorf("kernel resource limits unsupported")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return err
	}
	var virtual uint64
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "VmSize:" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return err
			}
			virtual = value * 1024
		}
	}
	if virtual == 0 {
		return fmt.Errorf("virtual memory unavailable")
	}
	for _, limit := range []struct {
		resource int
		bytes    uint64
	}{
		{syscall.RLIMIT_CORE, 0},
		{syscall.RLIMIT_DATA, investigateWorkerDataBytes},
		// Go reserves large PROT_NONE arenas (race builds reserve even more).
		// Bound additional address space independently of those startup mappings.
		{syscall.RLIMIT_AS, virtual + (512 << 20)},
	} {
		current := syscall.Rlimit{}
		if err := syscall.Getrlimit(limit.resource, &current); err != nil {
			return err
		}
		value := min(current.Max, limit.bytes)
		if err := syscall.Setrlimit(limit.resource, &syscall.Rlimit{Cur: value, Max: value}); err != nil {
			return err
		}
	}
	return nil
}

func configureInvestigateProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
