package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// Component loggers — created once at startup, zero per-call overhead.
var (
	loggingRootHandler slog.Handler
	logDNS             *slog.Logger
	logAdmin           *slog.Logger
	logCache           *slog.Logger
	logPolicy          *slog.Logger
	logLW              *slog.Logger // logwriter
	logArchiver        *slog.Logger
	logResolver        *slog.Logger
	logAuth            *slog.Logger
	logDB              *slog.Logger
	logBlocklist       *slog.Logger
	logAllowlist       *slog.Logger
	logSync            *slog.Logger
)

// initLogging reads LOG_FORMAT and LOG_LEVEL env vars and configures slog.
// Called twice: once before .env load (defaults), once after (picks up .env values).
func initLogging() {
	format := strings.ToLower(getEnv("LOG_FORMAT", "json"))
	levelStr := strings.ToLower(getEnv("LOG_LEVEL", "info"))

	var level slog.Level
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	loggingRootHandler = handler
	slog.SetDefault(slog.New(handler).With("service", "svart-dns"))
	recreateComponentLoggers()
}

// recreateComponentLoggers (re)creates all 13 component loggers from
// slog.Default(). Called by initLogging() and by initLoki() after wrapping
// the handler.
func recreateComponentLoggers() {
	logDNS = slog.Default().With("component", "dns")
	logAdmin = slog.Default().With("component", "admin")
	logCache = slog.Default().With("component", "cache")
	logPolicy = slog.Default().With("component", "policy")
	logLW = slog.Default().With("component", "logwriter")
	logArchiver = slog.Default().With("component", "archiver")
	logResolver = slog.Default().With("component", "resolver")
	logAuth = slog.Default().With("component", "auth")
	logDB = slog.Default().With("component", "db")
	logBlocklist = slog.Default().With("component", "blocklist")
	logAllowlist = slog.Default().With("component", "allowlist")
	logSync = slog.Default().With("component", "sync")
	updateDNSDebugFlag()
}

// fatal logs at Error level and exits. slog has no Fatal.
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// dnsDebugEnabled caches whether DNS debug logging is active,
// avoiding the context.Background() allocation on every check.
var dnsDebugEnabled bool

func updateDNSDebugFlag() {
	dnsDebugEnabled = logDNS.Enabled(context.Background(), slog.LevelDebug)
}
