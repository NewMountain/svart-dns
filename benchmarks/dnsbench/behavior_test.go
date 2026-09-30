package main

import (
	"io"
	"testing"
)

func TestRunRejectsInvalidLoadBeforeDriving(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config
		want string
	}{
		{"clients", config{queries: 1}, "-clients must be positive"},
		{"queries", config{clients: 1, queries: -1}, "-queries and -warmup must not be negative"},
		{"warmup", config{clients: 1, warmup: -1}, "-queries and -warmup must not be negative"},
		{"timeout", config{clients: 1, timeout: 0}, "-timeout must be positive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.cfg, io.Discard)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("run error=%v want%q", err, tc.want)
			}
		})
	}
}
