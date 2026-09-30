package svart

import (
	"crypto/tls"
	"errors"
	"net/http"
	"testing"
)

func TestSyncHTTPClientRefusesRedirects(t *testing.T) {
	savedConfig := syncTLSConfig
	savedConfigErr := syncTLSConfigErr
	savedAllowedPeers := syncAllowedPeers
	syncTLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	syncTLSConfigErr = nil
	syncAllowedPeers = map[string]struct{}{
		"https://peer.example.com:443": {},
	}
	defer func() {
		syncTLSConfig = savedConfig
		syncTLSConfigErr = savedConfigErr
		syncAllowedPeers = savedAllowedPeers
	}()

	client, err := newSyncHTTPClient("https://peer.example.com:443")
	if err != nil {
		t.Fatalf("newSyncHTTPClient: %v", err)
	}
	if client.CheckRedirect == nil {
		t.Fatal("sync client has no redirect policy; X-Sync-Key could be forwarded")
	}

	redirected, err := http.NewRequest(http.MethodGet, "https://attacker.example.com/api/sync", nil)
	if err != nil {
		t.Fatalf("new redirected request: %v", err)
	}
	redirected.Header.Set("X-Sync-Key", "must-not-be-forwarded")
	if err := client.CheckRedirect(redirected, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect error = %v, want http.ErrUseLastResponse", err)
	}
}
