# DD-024: TLS InsecureSkipVerify for trusted sync peers

**Status:** Superseded by [DD-028](DD-028-verified-sync-identity-and-non-disclosure-of-authentication-material.md)

**Status**: Superseded by DD-028. This section records the original deployment constraint and rationale; it is not current operational guidance.

**Historical decision**: The sync HTTP client set `InsecureSkipVerify: true` in its TLS config, skipping hostname verification for peer-to-peer communication.

**Why**: Production svart-dns nodes use wildcard Let's Encrypt certs (`*.example.lan`) which have no IP SANs. Peer URLs must use IP addresses (not hostnames) to avoid a chicken-and-egg problem: DNS rewrites that map hostnames to IPs are themselves part of the synced config, so hostname-based peer URLs can't resolve when sync is needed to restore those rewrites. With IP-based peer URLs and a wildcard cert, TLS hostname verification fails because the cert's CN/SAN (`*.example.lan`) doesn't match the IP (`10.42.1.6`).

At the time, peers were treated as explicitly trusted because they were admin-configured and authenticated via `SYNC_SECRET` (shared secret in `X-Sync-Key` header). That assumption was insufficient: an on-path endpoint could receive the shared secret and impersonate a peer when certificate identity was not verified.

**Revisit if**: Certs with IP SANs become available (would need cert-updater changes), or if the sync protocol moves to mTLS where client cert verification replaces shared secrets.
