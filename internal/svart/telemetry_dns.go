package svart

import (
	"context"
	"errors"
	"github.com/prometheus/client_golang/prometheus"
	"log/slog"
	"net"
	"time"
)

var dnsFullResponse = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "svart_dns_response_duration_seconds", Help: "Full DNS handler time through admission, logging queue and socket write (including rejected queries), using the coalesced clock.", Buckets: []float64{.00001, .00005, .0001, .0005, .001, .005, .01, .05, .1, .5, 1, 5}})
var dnsAdmissionDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "svart_dns_log_admission_duration_seconds", Help: "Time waiting for query log admission, including unsuccessful admission.", Buckets: []float64{.00001, .0001, .001, .01, .1, 1, 5}})
var dnsAdmission = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "svart_dns_log_admission_total", Help: "Query log admission outcomes; rejected requests receive SERVFAIL."}, []string{"outcome"})

func init()                              { prometheus.MustRegister(dnsFullResponse, dnsAdmissionDuration, dnsAdmission) }
func observeDNSResponse(start time.Time) { dnsFullResponse.Observe(clock.Now().Sub(start).Seconds()) }
func observeDNSAdmission(start time.Time, accepted bool) {
	dnsAdmissionDuration.Observe(clock.Now().Sub(start).Seconds())
	if accepted {
		dnsAdmission.WithLabelValues("accepted").Inc()
	} else {
		dnsAdmission.WithLabelValues("rejected").Inc()
	}
}

// Query identities are retained in owned storage; exporting them needs explicit opt-in.
func dnsDiagnosticLogger(logger *slog.Logger, clientIP, domain string) *slog.Logger {
	if !logQueryLines.Load() {
		return logger
	}
	if clientIP != "" {
		return logger.With("client_ip", clientIP, "domain", domain)
	}
	return logger.With("domain", domain)
}

// Error strings can embed questions, URLs and peer-controlled response data.
// Export only bounded classes unless query-detail logging was explicitly enabled.
func dnsErrorLogger(logger *slog.Logger, err error) *slog.Logger {
	if logQueryLines.Load() {
		return logger.With("error", err)
	}
	class := "exchange_failed"
	var networkError net.Error
	switch {
	case errors.Is(err, errNoEnabledUpstream):
		class = "no_enabled_upstream"
	case errors.Is(err, errAllUpstreamsFailed):
		class = "all_upstreams_failed"
	case errors.Is(err, errUpstreamReplyMismatch):
		class = "reply_mismatch"
	case errors.Is(err, errQueryNotForwardable):
		class = "not_forwardable"
	case errors.Is(err, errUpstreamSaturated), errors.Is(err, errResolverOverloaded):
		class = "overloaded"
	case errors.Is(err, errUnusableAnswer):
		class = "unusable_answer"
	case errors.Is(err, errFlightAborted):
		class = "resolution_aborted"
	case errors.Is(err, context.Canceled):
		class = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		class = "timeout"
	case errors.As(err, &networkError):
		if networkError.Timeout() {
			class = "timeout"
		} else {
			class = "network"
		}
	}
	return logger.With("error_class", class)
}
