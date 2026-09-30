# DD-004: Container-level DNS granularity is out of scope (for now)

**Decision**: DNS queries only carry source IP, not container identity. Containers sharing a bridge network all appear as the host IP. Distinguishing individual containers would require them to have distinct IPs (macvlan, separate subnets, etc). This is a network topology decision, not something solvable in DNS alone.

**Why**: This is a real limitation of the DNS protocol. We can't solve it without imposing network topology requirements on the user. Parking this for future investigation — possible approaches include integrating with container runtimes to map IPs to container names, or requiring macvlan for containers that need distinct policies.

**TODO**: Investigate Docker API / Podman API integration to auto-discover container-to-IP mappings for hosts that use macvlan or similar.
