# Final result tables

Read the [methods and limits](README.md) before comparing these measurements.

### Configured-product UDP comparison

Each cell is successful replies per second. Products retain their default history settings: Svart exact raw persistence is enabled, while Technitium query logging is disabled. These are not equal-durability engine comparisons.

| Scenario | Svart QPS | Pi-hole QPS | AdGuard Home QPS | Technitium QPS |
|---|---:|---:|---:|---:|
| Popular names, 50 workers | 85,300 | 12,901 | 46,626 | 120,989 |
| 20% list queries, 50 workers | 72,713 | 11,849 | 51,954 | 157,696 |
| 100% list queries | 54,160 | 9,014 | 54,645 | 129,941 |
| 10% list + 50% unique misses | 32,453 | 3,975 | 30,912 | 43,195 |
| 20% list queries, 200 workers | 44,933 | 8,783 | 61,018 | 156,535 |
| 64 effective clients, 5% misses | 37,261 | 4,699 | 59,074 | 166,337 |
| Fixed 10,000 offered QPS | 9,994 | 7,102 | 9,999 | 9,996 |

Fixed-rate scenario:

| Metric | Svart | Pi-hole | AdGuard Home | Technitium |
|---|---:|---:|---:|---:|
| p50 µs | 386.7 | 18,726.7 | 100.5 | 52.8 |
| p99 µs | 28,234.4 | 50,375.8 | 1,431.1 | 1,660.5 |
| RSS MiB after sequence | 748.5 | 712.7 | 446.9 | 509.7 |

The first Technitium fixed-rate scenario overlaps the unrelated workload from 08:02:16 UTC; its fixed-rate numbers are contended. Other first-order scenarios finished before that start time.

All nonzero failures/policy exceptions:

- svart/mixed-c200: timeouts 95, errors 0, unexpected allowed 0, unexpected blocked 0 of 300000.
- pihole/mixed-c200: timeouts 561, errors 0, unexpected allowed 0, unexpected blocked 0 of 300000.
- pihole/fixed-10k: timeouts 695, errors 0, unexpected allowed 0, unexpected blocked 0 of 300000.
- technitium/blocked: timeouts 0, errors 0, unexpected allowed 1, unexpected blocked 0 of 300000.

### Reverse-order observation with competing CPU load

Every row below overlaps an unrelated CPU-intensive workload. These observations are retained separately and are not averaged with the first order to claim a controlled ranking.

| Scenario | Svart QPS | Pi-hole QPS | AdGuard Home QPS | Technitium QPS |
|---|---:|---:|---:|---:|
| Popular names, 50 workers | 55,198 | 11,136 | 46,663 | 107,864 |
| 20% list queries, 50 workers | 58,039 | 11,481 | 40,304 | 148,668 |
| 100% list queries | 44,710 | 8,780 | 37,412 | 105,786 |
| 10% list + 50% unique misses | 20,725 | 3,215 | 18,743 | 29,340 |
| 20% list queries, 200 workers | 44,735 | 9,965 | 32,772 | 142,503 |
| 64 effective clients, 5% misses | 61,266 | 7,996 | 27,971 | 156,279 |
| Fixed 10,000 offered QPS | 9,999 | 7,064 | 9,999 | 9,999 |

Fixed-rate scenario:

| Metric | Svart | Pi-hole | AdGuard Home | Technitium |
|---|---:|---:|---:|---:|
| p50 µs | 174.4 | 17,177.8 | 187.3 | 67.0 |
| p99 µs | 15,345.1 | 57,417.7 | 5,465.0 | 2,115.5 |
| RSS MiB after sequence | 738.8 | 715.3 | 452.1 | 491.2 |

All nonzero failures/policy exceptions:

- technitium/blocked: timeouts 0, errors 0, unexpected allowed 1, unexpected blocked 0 of 300000.
- pihole/mixed-c200: timeouts 495, errors 0, unexpected allowed 0, unexpected blocked 0 of 300000.
- pihole/fixed-10k: timeouts 694, errors 0, unexpected allowed 0, unexpected blocked 0 of 300000.
- svart/mixed-c200: timeouts 71, errors 0, unexpected allowed 0, unexpected blocked 0 of 300000.


### Fresh logging pairs

| Mode | Build | Median replies/s | p50 range µs | p99 range µs | Peak RSS MiB |
|---|---|---:|---:|---:|---:|
| off | baseline | 159,419 | 227.9–278.9 | 2,973.4–4,122.9 | 169.0 |
| off | candidate-ab901e3 | 138,818 | 251.5–302.2 | 3,038.6–4,662.4 | 166.3 |
| on | baseline | 89,032 | 448.9–838.8 | 3,787.8–6,707.3 | 287.2 |
| on | candidate-ab901e3 | 64,595 | 494.4–605.3 | 4,647.7–5,140.9 | 306.6 |

### Direct query-line delivery

| Receiver/workload | Replies/s | p50 µs | p99 µs | Drain after recovery (s) | Verified raw record count |
|---|---:|---:|---:|---:|---:|
| final-logging-receiver-candidate-ab901e3-healthy | 63,665 | 613.5 | 5284.6 | 25.59 | 262,144 |
| final-logging-receiver-candidate-ab901e3-outage | 81,079 | 398.6 | 3990.0 | 24.87 | 262,144 |
| final-logging-receiver-candidate-ab901e3-slow | 67,929 | 567.0 | 4931.6 | 19.67 | 262,144 |
| final-logging-sustained-candidate-ab901e3-outage | 9,999 | 123.0 | 6053.4 | 46.79 | 1,000,000 |

Raw record counts include selected-field/name checks. Independent payload-byte
comparisons cover journal snapshots; the 983,040 records archived before the
sustained run's first snapshot lack an independent prearchive witness. See the
[oracle scope correction](README.md#recorded-results).
