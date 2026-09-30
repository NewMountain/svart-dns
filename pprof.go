package main

import (
	"net"
	"net/http"
	"net/http/pprof"
	"time"
)

// startPprof serves Go runtime profiles on a separate listener when PPROF_ADDR
// is set. It is off by default and refuses non-loopback addresses, because
// profiles expose memory contents (query names, client IPs) and must never be
// reachable from the network the DNS server faces.
func startPprof(addr string) {
	if addr == "" {
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		logAdmin.Error("PPROF_ADDR must be a loopback host:port; profiling disabled", "addr", addr)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	go func() {
		logAdmin.Info("pprof listening", "addr", addr)
		srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		if err := srv.ListenAndServe(); err != nil {
			logAdmin.Error("pprof listener stopped", "error", err)
		}
	}()
}
