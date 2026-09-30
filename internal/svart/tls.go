package svart

import (
	"crypto/tls"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var (
	currentCert   atomic.Pointer[tls.Certificate]
	certWatchStop chan struct{}
	certWatchDone chan struct{}
	certWatchOnce sync.Once
)

// loadCertificate loads a TLS certificate and key from disk.
func loadCertificate(certPath, keyPath string) (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// getCertificate is the tls.Config.GetCertificate callback.
// It returns the current in-memory certificate on every TLS handshake.
func getCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return currentCert.Load(), nil
}

// startCertWatcher launches a background goroutine that polls the cert file
// for mtime changes every 60s and reloads it. Polling is used instead of
// fsnotify because inotify doesn't work reliably on NFS mounts.
func startCertWatcher(certPath, keyPath string) {
	certWatchStop = make(chan struct{})
	certWatchDone = make(chan struct{})

	// Capture initial mtime
	var lastMtime time.Time
	if info, err := os.Stat(certPath); err == nil {
		lastMtime = info.ModTime()
	}

	ticker := time.NewTicker(60 * time.Second)

	go func() {
		defer close(certWatchDone)
		defer ticker.Stop()
		for {
			select {
			case <-certWatchStop:
				return
			case <-ticker.C:
				info, err := os.Stat(certPath)
				if err != nil {
					logAdmin.Error("cert watcher: failed to stat cert file", "path", certPath, "error", err)
					continue
				}
				mtime := info.ModTime()
				if mtime.Equal(lastMtime) {
					continue
				}
				cert, err := loadCertificate(certPath, keyPath)
				if err != nil {
					logAdmin.Error("cert watcher: failed to reload certificate", "error", err)
					continue
				}
				currentCert.Store(cert)
				lastMtime = mtime
				logAdmin.Info("reloaded TLS certificate", "cert", certPath)
			}
		}
	}()
}

// closeCertWatcher stops the cert watcher goroutine gracefully.
func closeCertWatcher() {
	certWatchOnce.Do(func() {
		if certWatchStop != nil {
			close(certWatchStop)
		}
		if certWatchDone != nil {
			<-certWatchDone
		}
	})
}
