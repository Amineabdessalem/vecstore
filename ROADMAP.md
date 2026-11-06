# Roadmap

- [ ] Write-ahead log so writes between snapshots survive a crash
- [x] Compaction: rebuild a collection's graph when soft-deleted nodes pass a threshold
- [x] Range filters on numeric metadata (`{"year": {"gte": 2020}}`)
- [x] Go client package (`client/`)
- [ ] Small Python client
- [x] Prometheus metrics endpoint (request latency, collection sizes)
- [ ] Recall/latency benchmark on a public dataset (e.g. SIFT1M subset) with an `ef` sweep
- [ ] Scalar quantisation (int8) to cut memory 4x
- [x] API keys via a bearer-token middleware
