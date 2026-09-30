package telemetry

import (
	"runtime/debug"
	"testing"
)

func TestBuildVersion(t *testing.T) {
	for _, tc := range []struct {
		info debug.BuildInfo
		want string
	}{
		{debug.BuildInfo{}, "development"},
		{debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, "v1.2.3"},
		{debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef"}, {Key: "vcs.modified", Value: "true"}}}, "0123456789abcdef-dirty"},
	} {
		if got := buildVersion(&tc.info); got != tc.want {
			t.Fatalf("version=%q want=%q", got, tc.want)
		}
	}
}

func TestVersionPrefersCompiledRevision(t *testing.T) {
	previous := buildRevision
	defer func() { buildRevision = previous }()
	buildRevision = "0123456789abcdef0123456789abcdef01234567"
	if got := Version(); got != buildRevision {
		t.Fatalf("identity=%q want compiled revision %q", got, buildRevision)
	}
}
