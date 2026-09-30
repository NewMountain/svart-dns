// dnsdiff sends the same A queries to two DNS servers and reports every name
// whose outcome differs (blocked vs answered, or a different rcode). It is
// the parity check for engine changes: run the old and the new binary with
// the same lists and compare what clients would actually get.
//
// Usage: dnsdiff -a 127.0.0.1:41053 -b 127.0.0.1:42053 < names.txt
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

func outcome(c *dns.Client, server, name string) string {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	for attempt := 0; attempt < 3; attempt++ {
		r, _, err := c.Exchange(m, server)
		if err != nil {
			continue
		}
		if r.Rcode == dns.RcodeNameError {
			return "blocked"
		}
		for _, rr := range r.Answer {
			if a, ok := rr.(*dns.A); ok && a.A.IsUnspecified() {
				return "blocked"
			}
		}
		return dns.RcodeToString[r.Rcode]
	}
	return "timeout"
}

func main() { os.Exit(command(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func command(args []string, input io.Reader, output, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("dnsdiff", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	a := flags.String("a", "", "first server host:port")
	b := flags.String("b", "", "second server host:port")
	workers := flags.Int("workers", 32, "concurrent lookups")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *workers <= 0 {
		if _, err := fmt.Fprintln(diagnostics, "dnsdiff: workers must be positive"); err != nil {
			return 1
		}
		return 2
	}

	var names []string
	sc := bufio.NewScanner(input)
	for sc.Scan() {
		if n := strings.TrimSpace(sc.Text()); n != "" {
			names = append(names, n)
		}
	}
	if err := sc.Err(); err != nil {
		if _, writeErr := fmt.Fprintln(diagnostics, "dnsdiff: input:", err); writeErr != nil {
			return 1
		}
		return 1
	}

	type res struct{ name, a, b string }
	out := make([]res, len(names))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &dns.Client{Timeout: 3 * time.Second}
			for i := range next {
				out[i] = res{names[i], outcome(c, *a, names[i]), outcome(c, *b, names[i])}
			}
		}()
	}
	for i := range names {
		next <- i
	}
	close(next)
	wg.Wait()
	counts := map[string]int{}
	diffs := 0
	for _, r := range out {
		counts["a:"+r.a]++
		counts["b:"+r.b]++
		if r.a != r.b {
			diffs++
			if _, err := fmt.Fprintf(output, "DIFF\t%s\t%s\t%s\n", r.name, r.a, r.b); err != nil {
				return 1
			}
		}
	}
	if _, err := fmt.Fprintf(diagnostics, "names %d, differing %d, outcomes %v\n", len(names), diffs, counts); err != nil {
		return 1
	}
	return 0
}
