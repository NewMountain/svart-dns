package main

import (
	"context"
	"log"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

func startDNSServer(ctx context.Context, network, addr string) error {
	server := &dns.Server{
		Addr:    addr,
		Net:     network,
		Handler: dns.HandlerFunc(handleDNSRequest),
	}

	go func() {
		<-ctx.Done()
		server.Shutdown()
	}()

	log.Printf("Starting DNS server on %s (%s)", addr, network)
	return server.ListenAndServe()
}

func handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	startTime := time.Now()
	clientIP := getClientIP(w)

	if len(r.Question) == 0 {
		msg := new(dns.Msg)
		msg.SetReply(r)
		w.WriteMsg(msg)
		return
	}

	q := r.Question[0]
	queryType := dns.TypeToString[q.Qtype]
	log.Printf("DNS Query from %s: %s %s", clientIP, q.Name, queryType)

	if rewriteResp := checkRewrite(r); rewriteResp != nil {
		log.Printf("Rewrite: %s -> %v", q.Name, rewriteResp.Answer)
		w.WriteMsg(rewriteResp)
		logQuery(clientIP, q.Name, queryType, dns.RcodeToString[rewriteResp.Rcode], false, "rewrite", time.Since(startTime).Milliseconds())
		return
	}

	if isBlockedForClient(clientIP, q.Name) {
		log.Printf("Blocked: %s for client %s", q.Name, clientIP)
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(msg)

		logQuery(clientIP, q.Name, queryType, "NXDOMAIN", true, "", time.Since(startTime).Milliseconds())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := resolveQuery(ctx, r)
	if err != nil {
		log.Printf("Resolution failed for %s: %v", q.Name, err)
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.SetRcode(r, dns.RcodeServerFailure)
		w.WriteMsg(msg)

		logQuery(clientIP, q.Name, queryType, "SERVFAIL", false, "", time.Since(startTime).Milliseconds())
		return
	}

	if isResponseBlockedByIP(clientIP, resp) {
		log.Printf("Blocked by IP: %s for client %s", q.Name, clientIP)
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(msg)

		logQuery(clientIP, q.Name, queryType, "NXDOMAIN", true, "", time.Since(startTime).Milliseconds())
		return
	}

	w.WriteMsg(resp)

	rcode := dns.RcodeToString[resp.Rcode]
	latency := time.Since(startTime).Milliseconds()
	log.Printf("Resolved %s -> %s (latency: %dms)", q.Name, rcode, latency)

	logQuery(clientIP, q.Name, queryType, rcode, false, "", latency)
}

func logQuery(clientIP, queryName, queryType, responseCode string, blocked bool, upstream string, latencyMs int64) {
	go func() {
		_, err := db.Exec(`
			INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, upstream, latency_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, clientIP, queryName, queryType, responseCode, blocked, upstream, latencyMs)

		if err != nil {
			log.Printf("Failed to log query: %v", err)
		}
	}()
}

func getClientIP(w dns.ResponseWriter) string {
	addr := w.RemoteAddr()
	switch v := addr.(type) {
	case *net.UDPAddr:
		return v.IP.String()
	case *net.TCPAddr:
		return v.IP.String()
	default:
		host, _, _ := net.SplitHostPort(addr.String())
		return host
	}
}

func checkRewrite(r *dns.Msg) *dns.Msg {
	if len(r.Question) == 0 {
		return nil
	}

	q := r.Question[0]
	domain := strings.ToLower(strings.TrimSuffix(q.Name, "."))

	val, ok := rewritesCache.Load(domain)
	if !ok {
		return nil
	}

	rewrite := val.(Rewrite)
	ips := rewrite.IPAddresses

	if len(ips) == 0 {
		return nil
	}

	msg := new(dns.Msg)
	msg.SetReply(r)

	if q.Qtype == dns.TypeA {
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip != nil && ip.To4() != nil {
				rr := &dns.A{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					A: ip.To4(),
				}
				msg.Answer = append(msg.Answer, rr)
			}
		}
	} else if q.Qtype == dns.TypeAAAA {
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip != nil && ip.To4() == nil {
				rr := &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					AAAA: ip,
				}
				msg.Answer = append(msg.Answer, rr)
			}
		}
	}

	if len(msg.Answer) > 0 {
		return msg
	}

	return nil
}
