package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Bootstrap resolution turns upstream hostnames (tls://dns.quad9.net,
// https://doh.mullvad.net/...) into IPs using the configured bootstrap
// servers, never the system resolver, which may point back at svart.

const (
	// bootstrapNegativeTTL is how long a failed lookup is remembered. Without
	// it every query to an upstream whose name does not resolve re-queried
	// every bootstrap server; short, so a fixed bootstrap recovers quickly.
	bootstrapNegativeTTL = 10 * time.Second

	// bootstrapQueryTimeout caps one bootstrap server attempt, so a silent
	// first server still leaves time for the next within a request deadline.
	bootstrapQueryTimeout = 2 * time.Second

	// A shared cold lookup has its own bound, independent of any one caller.
	bootstrapLookupTimeout = 10 * time.Second

	// maxCNAMEHops bounds CNAME chain walking in a bootstrap answer.
	maxCNAMEHops = 8
)

type bootstrapEntry struct {
	ip        string // set when err is nil
	err       error
	expiresAt time.Time
}

// bootstrapHosts caches lookup results, including failures. Keys come only
// from configured upstreams, never from client queries, so the map is
// bounded by configuration and an expired entry is simply replaced.
var bootstrapHosts sync.Map // lowercase hostname -> bootstrapEntry

var bootstrapClient = &dns.Client{Net: "udp", Timeout: bootstrapQueryTimeout}

var errNoBootstrapServers = errors.New("no bootstrap servers configured")

// resolveUpstreamHostname returns address with its host replaced by an IP
// from the bootstrap servers. IP literals pass through untouched.
func resolveUpstreamHostname(ctx context.Context, address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		host, port = address, ""
	}
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return address, nil
	}
	ip, err := bootstrapLookup(ctx, host)
	if err != nil {
		return "", fmt.Errorf("bootstrap %s: %w", host, err)
	}
	if port == "" {
		return ip, nil
	}
	return net.JoinHostPort(ip, port), nil
}

func bootstrapLookup(ctx context.Context, host string) (string, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	now := clock.Now()
	if v, ok := bootstrapHosts.Load(host); ok {
		if e := checkedBootstrapEntry(v); now.Before(e.expiresAt) {
			return e.ip, e.err
		}
	}

	bootstrapFlights.Lock()
	// Recheck after joining the cold path: the prior owner may have completed.
	if v, ok := bootstrapHosts.Load(host); ok {
		if e := checkedBootstrapEntry(v); clock.Now().Before(e.expiresAt) {
			bootstrapFlights.Unlock()
			return e.ip, e.err
		}
	}
	key := bootstrapFlightKey{host: host, generation: bootstrapFlights.generation}
	flight := bootstrapFlights.pending[key]
	if flight == nil {
		shared, cancel := context.WithTimeout(context.Background(), bootstrapLookupTimeout)
		flight = &bootstrapFlight{done: make(chan struct{}), cancel: cancel}
		bootstrapFlights.pending[key] = flight
		// Capture this generation's database before starting work. The operation
		// owns no caller's deadline; every caller waits through its own context.
		go completeBootstrapFlight(shared, readDB, key, flight)
	}
	flight.waiters++
	bootstrapFlights.Unlock()

	defer func() {
		bootstrapFlights.Lock()
		defer bootstrapFlights.Unlock()
		flight.waiters--
		if flight.waiters == 0 && bootstrapFlights.pending[key] == flight {
			// Nobody still needs this operation. A later caller starts fresh and
			// cannot inherit its cancellation or cache its late network error.
			delete(bootstrapFlights.pending, key)
			flight.cancel()
		}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-flight.done:
		return flight.ip, flight.err
	}
}

func completeBootstrapFlight(ctx context.Context, source *sql.DB, key bootstrapFlightKey, flight *bootstrapFlight) {
	defer flight.cancel()
	ip, err := resolveWithBootstrap(ctx, source, key.host)
	bootstrapFlights.Lock()
	defer bootstrapFlights.Unlock()
	// Abandoned or old-generation operations must not repopulate the cache.
	current := bootstrapFlights.pending[key] == flight
	if current && key.generation == bootstrapFlights.generation && ctx.Err() == nil {
		switch err {
		case nil:
			if ttl := getBootstrapTTL(); ttl > 0 {
				bootstrapHosts.Store(key.host, bootstrapEntry{ip: ip, expiresAt: clock.Now().Add(time.Duration(ttl) * time.Second)})
			}
		default:
			bootstrapHosts.Store(key.host, bootstrapEntry{err: err, expiresAt: clock.Now().Add(bootstrapNegativeTTL)})
		}
	}
	flight.ip, flight.err = ip, err
	if current {
		delete(bootstrapFlights.pending, key)
	}
	close(flight.done)
}

type bootstrapFlightKey struct {
	host       string
	generation uint64
}
type bootstrapFlight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	ip      string
	err     error
}

var bootstrapFlights = struct {
	sync.Mutex
	generation uint64
	pending    map[bootstrapFlightKey]*bootstrapFlight
}{pending: make(map[bootstrapFlightKey]*bootstrapFlight)}

func invalidateBootstrapCache() {
	bootstrapFlights.Lock()
	defer bootstrapFlights.Unlock()
	bootstrapFlights.generation++
	bootstrapHosts.Clear()
}

func resolveWithBootstrap(ctx context.Context, source *sql.DB, host string) (string, error) {
	servers, err := bootstrapServers(ctx, source)
	if err != nil {
		return "", err
	}
	if len(servers) == 0 {
		return "", errNoBootstrapServers
	}

	lastErr := fmt.Errorf("no bootstrap server returned an address for %s", host)
	for _, server := range servers {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		q := new(dns.Msg)
		q.SetQuestion(dns.Fqdn(host), dns.TypeA) // fresh random ID per server
		r, err := exchangeBootstrap(ctx, q, server)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			logResolver.Warn("bootstrap DNS query failed", "server", server, "host", host, "error", err)
			lastErr = err
			continue
		}
		// Bootstrap replies are plaintext UDP: validate them like any
		// upstream reply before trusting an address.
		if err := validateUpstreamReply(q, r); err != nil {
			if errors.Is(err, errUpstreamReplyMismatch) {
				dnsUpstreamMismatchedReplies.WithLabelValues(server).Inc()
			}
			lastErr = err
			continue
		}
		if r.Rcode != dns.RcodeSuccess {
			lastErr = fmt.Errorf("bootstrap server %s answered %s", server, dns.RcodeToString[r.Rcode])
			continue
		}
		if ip, ok := addressFor(r, q.Question[0].Name); ok {
			return ip, nil
		}
	}
	return "", lastErr
}

// Closing the socket interrupts a canceled flight immediately. The DNS client's
// exchange deadline alone does not observe context cancellation during a read.
func exchangeBootstrap(ctx context.Context, q *dns.Msg, server string) (*dns.Msg, error) {
	conn, err := bootstrapClient.DialContext(ctx, server)
	if err != nil {
		return nil, err
	}
	defer closeUpstreamTransport(conn)
	stop := context.AfterFunc(ctx, func() { closeUpstreamTransport(conn) })
	defer stop()
	reply, _, err := bootstrapClient.ExchangeWithConnContext(ctx, q, conn)
	return reply, err
}

// bootstrapServers reads the configured servers through the read pool: the
// write pool has one connection, and a long config write must not stall
// upstream resolution.
func bootstrapServers(ctx context.Context, source *sql.DB) ([]string, error) {
	rows, err := source.QueryContext(ctx, "SELECT server FROM bootstrap_servers ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("read bootstrap servers: %w", err)
	}
	defer closeQueryRows(rows)

	var servers []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("read bootstrap servers: %w", err)
		}
		if s = strings.TrimSpace(s); s != "" {
			servers = append(servers, withDefaultPort(s, "53"))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read bootstrap servers: %w", err)
	}
	return servers, nil
}

// addressFor returns the first A record owned by name, following name's
// CNAME chain within the answer. Records for any other owner are ignored:
// a reply may not assign addresses to names nobody asked about.
func addressFor(r *dns.Msg, name string) (string, bool) {
	for hop := 0; hop <= maxCNAMEHops; hop++ {
		next := ""
		for _, rr := range r.Answer {
			if !strings.EqualFold(rr.Header().Name, name) {
				continue
			}
			switch v := rr.(type) {
			case *dns.A:
				return v.A.String(), true
			case *dns.CNAME:
				next = v.Target
			}
		}
		if next == "" {
			return "", false
		}
		name = next
	}
	return "", false
}

// The bootstrap cache stores only completed bootstrapEntry values.
func checkedBootstrapEntry(value any) bootstrapEntry {
	entry, ok := value.(bootstrapEntry)
	if !ok {
		panic("bootstrap cache has an invalid internal type")
	}
	return entry
}
