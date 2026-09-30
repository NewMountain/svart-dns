package telemetry

import (
	"fmt"
	"log/slog"
)

// The SDK's variadic logging interface is translated at this external boundary.
type profileLogger struct{}

func (profileLogger) Infof(format string, args ...interface{}) {
	slog.Info("profiler", "detail", fmt.Sprintf(format, args...))
}
func (profileLogger) Debugf(format string, args ...interface{}) {
	slog.Debug("profiler", "detail", fmt.Sprintf(format, args...))
}
func (profileLogger) Errorf(format string, args ...interface{}) {
	slog.Error("profile export failed", "detail", fmt.Sprintf(format, args...))
}
