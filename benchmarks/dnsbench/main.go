// dnsbench is a closed-loop DNS load generator for apples-to-apples comparison
// of svart-dns, Pi-hole, AdGuard Home and Technitium.
//
// Each worker owns one long-lived UDP socket and keeps exactly one query in
// flight, so the numbers measure the server rather than per-query socket setup
// in the client. Inputs are local files only; nothing is downloaded.
//
// Query mix (percentages of -queries):
//   - blocked: random names from -blocklist (should be sinkholed)
//   - miss:    unique random names under -miss-zone (always a cache miss)
//   - rest:    popular domains (cache hits after the first lookup)
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

type config struct {
	target        string
	label         string
	clients       int
	queries       int
	warmup        int
	blockedPct    int
	missPct       int
	missZone      string
	blocklist     string
	timeout       time.Duration
	jsonOut       bool
	sources       int     // distinct source addresses: sourceNet + 2 ...
	sourceNet     string  // IPv4 network the sources come from
	rate          float64 // total queries/s across workers; 0 = closed loop
	completionURL string
}

type outcome uint8

const (
	outcomeAnswered outcome = iota
	outcomeBlocked
	outcomeTimeout
	outcomeError
)

type sample struct {
	latency time.Duration
	outcome outcome
}

// Summary is the machine-readable result of one run.
type Summary struct {
	RepliesCompleteUnixNS int64               `json:"replies_complete_unix_ns,omitempty"`
	Completion            *CompletionSnapshot `json:"completion,omitempty"`
	Label                 string              `json:"label"`
	Target                string              `json:"target"`
	Clients               int                 `json:"clients"`
	Queries               int                 `json:"queries"`
	Answered              int                 `json:"answered"`
	Blocked               int                 `json:"blocked"`
	Timeouts              int                 `json:"timeouts"`
	Errors                int                 `json:"errors"`
	WallSec               float64             `json:"wall_seconds"`
	QPS                   float64             `json:"qps"`
	P50Micros             float64             `json:"p50_us"`
	P95Micros             float64             `json:"p95_us"`
	P99Micros             float64             `json:"p99_us"`
	P999Micros            float64             `json:"p999_us"`
	MaxMicros             float64             `json:"max_us"`
	ExpectedBlocked       int                 `json:"expected_blocked"`
	UnexpectedAllowed     int                 `json:"unexpected_allowed"`
	UnexpectedBlocked     int                 `json:"unexpected_blocked"`
	BlockedPct            int                 `json:"blocked_pct"`
	MissPct               int                 `json:"miss_pct"`
}

func main() { os.Exit(command(os.Args[1:], os.Stdout, os.Stderr)) }

func command(args []string, output, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("dnsbench", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	var c config
	server := flags.String("server", "127.0.0.1", "DNS server address")
	port := flags.Int("port", 5353, "DNS server port")
	flags.IntVar(&c.clients, "clients", 50, "concurrent workers, one socket and one in-flight query each")
	flags.IntVar(&c.queries, "queries", 200000, "measured queries")
	flags.IntVar(&c.warmup, "warmup", 5000, "unmeasured warmup queries (fills caches)")
	flags.IntVar(&c.blockedPct, "blocked-pct", 20, "percent of queries drawn from -blocklist")
	flags.IntVar(&c.missPct, "miss-pct", 0, "percent of queries that are unique names (forced cache misses)")
	flags.StringVar(&c.missZone, "miss-zone", "bench-miss.example", "parent zone for unique miss names")
	flags.StringVar(&c.blocklist, "blocklist", "", "domain-per-line or hosts-format file (required when -blocked-pct > 0)")
	flags.DurationVar(&c.timeout, "timeout", 2*time.Second, "per-query timeout")
	flags.StringVar(&c.label, "label", "", "label for the results")
	flags.BoolVar(&c.jsonOut, "json", false, "print a single JSON summary line instead of text")
	flags.IntVar(&c.sources, "sources", 0, "send from N distinct source addresses (-source-net + 2 up) so the server sees N clients; 0 = OS default")
	flags.StringVar(&c.sourceNet, "source-net", "127.0.0.0", "IPv4 network for -sources; the addresses must be assigned locally (e.g. to lo)")
	flags.Float64Var(&c.rate, "rate", 0, "open-loop total rate in queries/s (latency at a fixed load); 0 = closed loop (max throughput)")
	flags.StringVar(&c.completionURL, "completion-url", "", "GET a metrics snapshot after replies, before summarizing (requires -json)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if ip := net.ParseIP(c.sourceNet).To4(); ip == nil {
		if _, err := fmt.Fprintf(diagnostics, "dnsbench: -source-net %q is not an IPv4 address\n", c.sourceNet); err != nil {
			return 1
		}
		return 2
	}
	c.target = net.JoinHostPort(*server, fmt.Sprint(*port))
	if c.label == "" {
		c.label = c.target
	}
	if err := run(c, output); err != nil {
		if _, writeErr := fmt.Fprintln(diagnostics, "dnsbench:", err); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

func run(c config, output io.Writer) error {
	if c.completionURL != "" && !c.jsonOut {
		return errors.New("-completion-url requires -json to retain the complete response")
	}
	if c.clients <= 0 {
		return errors.New("-clients must be positive")
	}
	if c.queries < 0 || c.warmup < 0 {
		return errors.New("-queries and -warmup must not be negative")
	}
	if c.timeout <= 0 {
		return errors.New("-timeout must be positive")
	}
	if c.blockedPct+c.missPct > 100 || c.blockedPct < 0 || c.missPct < 0 {
		return errors.New("-blocked-pct + -miss-pct must be between 0 and 100")
	}
	var blocked []string
	if c.blockedPct > 0 && c.blocklist == "" {
		return errors.New("-blocklist is required when -blocked-pct > 0")
	}
	if c.blocklist != "" {
		var err error
		if blocked, err = loadDomains(c.blocklist); err != nil {
			return err
		}
		if len(blocked) == 0 {
			return fmt.Errorf("no domains parsed from %s", c.blocklist)
		}
	}
	rng := rand.New(rand.NewPCG(1, 2)) // #nosec G404 -- Repeatable benchmark query selection is not a security decision.
	if c.warmup > 0 {
		warm := buildQueries(rng, c, blocked, c.warmup)
		_, _ = drive(c, warm)
	}
	queries := buildQueries(rng, c, blocked, c.queries)
	samples, wall := drive(c, queries)
	repliesComplete := time.Now().UnixNano()
	completion, completionErr := captureCompletion(c.completionURL)
	s := summarize(c, samples, wall)
	s.RepliesCompleteUnixNS = repliesComplete
	s.Completion = completion
	expected := make(map[string]bool, len(blocked))
	for _, domain := range blocked {
		expected[domain] = true
	}
	for i, query := range queries {
		if expected[query] {
			s.ExpectedBlocked++
			if samples[i].outcome == outcomeAnswered {
				s.UnexpectedAllowed++
			}
		} else if samples[i].outcome == outcomeBlocked {
			s.UnexpectedBlocked++
		}
	}
	if c.jsonOut {
		return errors.Join(completionErr, json.NewEncoder(output).Encode(s))
	}
	return printText(output, s)
}

func loadDomains(path string) (domains []string, resultErr error) {
	f, err := os.Open(path) // #nosec G304 -- This local CLI intentionally reads the operator-selected blocklist file.
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		fields := strings.Fields(line)
		d := fields[0]
		if len(fields) >= 2 && (d == "0.0.0.0" || d == "127.0.0.1" || d == "::") {
			d = fields[1]
		}
		d = strings.TrimPrefix(strings.TrimPrefix(d, "*."), "||")
		d = strings.TrimSuffix(strings.TrimSuffix(d, "^"), ".")
		if d == "" || d == "localhost" || strings.ContainsAny(d, "/*$") {
			continue
		}
		out = append(out, dns.Fqdn(strings.ToLower(d)))
	}
	return out, sc.Err()
}

func buildQueries(rng *rand.Rand, c config, blocked []string, n int) []string {
	out := make([]string, n)
	for i := range out {
		roll := rng.IntN(100)
		switch {
		case roll < c.blockedPct:
			out[i] = blocked[rng.IntN(len(blocked))]
		case roll < c.blockedPct+c.missPct:
			out[i] = fmt.Sprintf("q%016x.%s.", rng.Uint64(), c.missZone)
		default:
			out[i] = dns.Fqdn(legitimateDomains[rng.IntN(len(legitimateDomains))])
		}
	}
	return out
}

func drive(c config, queries []string) ([]sample, time.Duration) {
	samples := make([]sample, len(queries))
	var next atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < c.clients; w++ {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			worker(c, queries, samples, &next, seed)
		}(uint64(w) + 1)
	}
	wg.Wait()
	return samples, time.Since(start)
}

func worker(c config, queries []string, samples []sample, next *atomic.Int64, seed uint64) {
	var laddr *net.UDPAddr
	if c.sources > 0 {
		base := net.ParseIP(c.sourceNet).To4()
		n := (seed-1)%uint64(c.sources) + 2
		laddr = &net.UDPAddr{IP: net.IPv4(base[0], base[1], base[2]+byte((n>>8)&255), base[3]+byte(n&255))}
	}
	raddr, err := net.ResolveUDPAddr("udp", c.target)
	if err != nil {
		for i := next.Add(1) - 1; i < int64(len(queries)); i = next.Add(1) - 1 {
			samples[i] = sample{outcome: outcomeError}
		}
		return
	}
	conn, err := net.DialUDP("udp", laddr, raddr)
	if err != nil {
		for i := next.Add(1) - 1; i < int64(len(queries)); i = next.Add(1) - 1 {
			samples[i] = sample{outcome: outcomeError}
		}
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("dnsbench: close UDP socket: %v", err)
		}
	}()
	rng := rand.New(rand.NewPCG(seed, seed*7919)) // #nosec G404 -- Repeatable local benchmark IDs are not secrets or authentication tokens.
	buf := make([]byte, 4096)
	msg := new(dns.Msg)
	start := time.Now()
	for i := next.Add(1) - 1; i < int64(len(queries)); i = next.Add(1) - 1 {
		if c.rate > 0 {
			// Query i is due at i/rate; a worker that is late sends at once, so
			// latency starts at send time and excludes generator scheduling delay.
			if wait := time.Until(start.Add(time.Duration(float64(i) / c.rate * float64(time.Second)))); wait > 0 {
				time.Sleep(wait)
			}
		}
		msg.SetQuestion(queries[i], dns.TypeA)
		msg.Id = uint16(rng.Uint32() & 65535)
		wire, err := msg.Pack()
		if err != nil {
			samples[i] = sample{outcome: outcomeError}
			continue
		}
		t0 := time.Now()
		deadline := t0.Add(c.timeout)
		if err := conn.SetDeadline(deadline); err != nil {
			samples[i] = sample{outcome: outcomeError, latency: time.Since(t0)}
			continue
		}
		if _, err := conn.Write(wire); err != nil {
			samples[i] = sample{outcome: outcomeError, latency: time.Since(t0)}
			continue
		}
		samples[i] = readReply(conn, buf, msg.Id, t0)
	}
}

// readReply reads until the reply matching id arrives, discarding late replies
// to earlier timed-out queries on the same socket.
func readReply(conn net.Conn, buf []byte, id uint16, t0 time.Time) sample {
	for {
		n, err := conn.Read(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return sample{outcome: outcomeTimeout, latency: time.Since(t0)}
			}
			return sample{outcome: outcomeError, latency: time.Since(t0)}
		}
		if n < 12 || uint16(buf[0])<<8|uint16(buf[1]) != id {
			continue
		}
		lat := time.Since(t0)
		resp := new(dns.Msg)
		if err := resp.Unpack(buf[:n]); err != nil {
			return sample{outcome: outcomeError, latency: lat}
		}
		// This harness measures complete UDP replies. A TC response asks the
		// client to retry over TCP; counting it would inflate UDP success/QPS.
		if resp.Truncated || resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError {
			return sample{outcome: outcomeError, latency: lat}
		}
		if isBlocked(resp) {
			return sample{outcome: outcomeBlocked, latency: lat}
		}
		return sample{outcome: outcomeAnswered, latency: lat}
	}
}

func isBlocked(resp *dns.Msg) bool {
	if resp.Rcode == dns.RcodeNameError {
		return true
	}
	for _, rr := range resp.Answer {
		switch a := rr.(type) {
		case *dns.A:
			if a.A.IsUnspecified() {
				return true
			}
		case *dns.AAAA:
			if a.AAAA.IsUnspecified() {
				return true
			}
		}
	}
	return false
}

func summarize(c config, samples []sample, wall time.Duration) Summary {
	s := Summary{Label: c.label, Target: c.target, Clients: c.clients, Queries: len(samples),
		WallSec: wall.Seconds(), BlockedPct: c.blockedPct, MissPct: c.missPct}
	lat := make([]time.Duration, 0, len(samples))
	for _, x := range samples {
		switch x.outcome {
		case outcomeAnswered:
			s.Answered++
			lat = append(lat, x.latency)
		case outcomeBlocked:
			s.Blocked++
			lat = append(lat, x.latency)
		case outcomeTimeout:
			s.Timeouts++
		case outcomeError:
			s.Errors++
		}
	}
	slices.Sort(lat)
	us := func(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1000 }
	pct := func(p float64) float64 {
		if len(lat) == 0 {
			return 0
		}
		return us(lat[min(len(lat)-1, int(float64(len(lat))*p))])
	}
	s.QPS = float64(s.Answered+s.Blocked) / wall.Seconds()
	s.P50Micros, s.P95Micros, s.P99Micros, s.P999Micros = pct(0.50), pct(0.95), pct(0.99), pct(0.999)
	if len(lat) > 0 {
		s.MaxMicros = us(lat[len(lat)-1])
	}
	return s
}

func printText(output io.Writer, s Summary) error {
	report := fmt.Sprintf("=== %s (%s) ===\nqueries %d  clients %d  mix blocked=%d%% miss=%d%%\nanswered %d  blocked %d  timeouts %d  errors %d\nthroughput %.0f qps over %.2fs\nlatency p50 %.0fµs  p95 %.0fµs  p99 %.0fµs  p99.9 %.0fµs  max %.0fµs\n",
		s.Label, s.Target, s.Queries, s.Clients, s.BlockedPct, s.MissPct, s.Answered, s.Blocked, s.Timeouts, s.Errors, s.QPS, s.WallSec, s.P50Micros, s.P95Micros, s.P99Micros, s.P999Micros, s.MaxMicros)
	_, err := io.WriteString(output, report)
	return err
}
