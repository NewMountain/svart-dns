package telemetry

import "runtime/debug"

// buildRevision is set by release builders using -ldflags=-X with the immutable source SHA.
// It is compile-time provenance, never operator-controlled runtime label input.
var buildRevision string

// Version identifies the executable's source without accepting arbitrary telemetry labels.
func Version() string {
	if buildRevision != "" {
		return buildRevision
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return buildVersion(info)
}
func buildVersion(info *debug.BuildInfo) string {
	version := info.Main.Version
	if version == "" || version == "(devel)" {
		version = "development"
	}
	dirty := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			version = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if dirty {
		version += "-dirty"
	}
	return version
}
