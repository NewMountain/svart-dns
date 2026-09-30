# DD-022: Peer pairing + gossip-based mesh discovery

**Decision**: Replace manual bidirectional peer registration with a two-part system: (1) a pairing handshake for initial peer connection, and (2) gossip-based auto-discovery for mesh formation. Pairing uses a three-endpoint flow with one-time codes. Gossip embeds `SelfURL` and `KnownPeers` in every sync response, allowing nodes to discover and auto-add unknown peers.

**Pairing protocol**:

1. Node A: `POST /api/peers/pair` → generates a 16-byte hex code (10 min expiry), returns code + self URL
2. Admin enters code + Node A's URL on Node B: `POST /api/peers/confirm` → Node B calls Node A's `POST /api/sync/pair/complete` with the code, Node B's canonical URL, and a domain-separated HMAC-SHA256 proof keyed by its local sync secret
3. Node A verifies that proof in constant time, consumes the one-time code, registers Node B, and returns a distinct initiator-role proof. Node B verifies the response proof before registering Node A. One pairing configures both directions.

Each proof is bound to protocol version `svart-dns-pairing-v1`, sender role, one-time code, and the canonical confirmer URL. A proof therefore cannot be replayed in the opposite direction or against a different URL. The raw sync secret is never placed in a pairing request or response. Pairing is available only when the local secret is at least 32 bytes.

**Gossip discovery**: Every `SyncResponse` includes the responding node's `SelfURL` (constructed from `EXTERNAL_IP` or auto-detected IP + admin port) and `KnownPeers` (all configured peer URLs). `discoverPeers()` canonicalizes every candidate and rejects it unless it falls inside the same TLS and exact `SYNC_PEER_ALLOWLIST` boundary as a manually configured peer. Within that pre-authorized set, a new peer is auto-added through `addPeerToSettings()` unless it is already known or explicitly deleted. Gossip can therefore complete a pre-approved mesh, but a compromised peer cannot turn attacker-selected HTTPS destinations into sync targets.

**Canonical peer URLs**: Peer identity is the canonical HTTPS origin: lowercase host plus explicit port (`https://10.42.1.7:443`). Userinfo, non-root paths, query strings, and fragments are rejected; a missing port becomes `443` and a lone `/` is removed. Production lists every permitted node origin, including each node's own `self_url`, in `SYNC_PEER_ALLOWLIST`. Manual add, pairing, gossip, and the sync HTTP client all enforce that boundary.

**External IP detection**: `detectExternalIP()` scans network interfaces, skipping loopback, link-local, Docker bridge interfaces (docker*, br-*, veth*, cni*, flannel*, podman*), and IPv6. `EXTERNAL_IP` env var overrides auto-detection. Used for `selfURL()` construction and loopback remapping in query logs.

**Why**: Manual bidirectional peer configuration is error-prone and doesn't scale. Mutual HMAC proofs confirm both nodes possess the same strong secret without disclosing that secret to an endpoint selected by an administrator or reflected by a malicious peer. Gossip avoids repeated registration inside an operator-approved topology, while the exact destination allowlist prevents it from becoming a secret-exfiltration oracle.

**Revisit if**: Mesh grows beyond ~10 nodes where gossip overhead matters (unlikely for homelab DNS), or if dynamic membership is required. Dynamic membership would need a signed enrollment mechanism or comparable authorization; weakening the exact destination boundary is not sufficient.
