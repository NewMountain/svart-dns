# DD-005: No recursive DNS resolution

**Decision**: Use forwarding-only resolution to upstream providers (Mullvad, Quad9, Cloudflare, etc.) over encrypted protocols (DoH, DoT). No recursive resolution from root servers.

**Why**: Recursive resolution is mostly unencrypted (port 53 plaintext), making it a bigger privacy leak than encrypted forwarding to trusted providers. The privacy gain from "not trusting any single provider" is achieved by distributing queries across multiple providers, not by doing our own recursion. Speed is also a factor — recursive resolution involves multiple round-trips vs a single encrypted hop to a provider that has its own cache.

**Revisit if**: Encrypted recursive resolution becomes practical, or if we lose trust in upstream providers (censorship, record manipulation).
