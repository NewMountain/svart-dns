| Window / panel | Baseline miss (s) | Final miss (s) |
|---|---:|---:|
| /api/stats/timeseries?buckets=1&window=1h | 0.230 | 0.244 |
| /api/stats/timeseries?buckets=24&window=1h | 0.111 | 0.202 |
| /api/stats/top-clients?limit=8&window=1h | 0.187 | 0.186 |
| /api/stats/block-sources?limit=10&window=1h | 0.050 | 0.040 |
| /api/stats/upstream-usage?limit=10&window=1h | 0.048 | 0.038 |
| /api/stats/top-domains?limit=10&window=1h | 2.145 | 0.112 |
| /api/stats/latency?buckets=24&window=1h | 0.122 | 0.109 |
| /api/stats/servfails?limit=10&window=1h | 0.034 | 0.037 |
| /api/stats/timeseries?buckets=1&window=24h | 4.006 | 4.002 |
| /api/stats/timeseries?buckets=24&window=24h | 3.935 | 4.526 |
| /api/stats/top-clients?limit=8&window=24h | 4.625 | 5.719 |
| /api/stats/block-sources?limit=10&window=24h | 1.383 | 2.379 |
| /api/stats/upstream-usage?limit=10&window=24h | 1.242 | 1.296 |
| /api/stats/top-domains?limit=10&window=24h | 2.534 | 4.929 |
| /api/stats/latency?buckets=24&window=24h | 4.923 | 3.845 |
| /api/stats/servfails?limit=10&window=24h | 0.645 | 0.845 |
| /api/stats/timeseries?buckets=1&window=7d | 21.483 | 27.901 |
| /api/stats/timeseries?buckets=24&window=7d | 33.889 | 34.440 |
| /api/stats/top-clients?limit=8&window=7d | 24.509 | 27.848 |
| /api/stats/block-sources?limit=10&window=7d | 10.739 | 7.628 |
| /api/stats/upstream-usage?limit=10&window=7d | 9.059 | 8.674 |
| /api/stats/top-domains?limit=10&window=7d | 35.487 | 25.140 |
| /api/stats/latency?buckets=24&window=7d | 43.636 | 44.027 |
| /api/stats/servfails?limit=10&window=7d | 7.657 | 6.166 |
