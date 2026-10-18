# Roadmap

- [ ] Write-ahead log so writes between snapshots survive a crash
- [ ] Compaction: rebuild a collection's graph when soft-deleted nodes pass a threshold
- [ ] Range filters on numeric metadata (`{"year": {"gte": 2020}}`)
- [ ] Go client package and a small Python client
- [ ] Prometheus metrics endpoint (request latency, collection sizes)
- [ ] Recall/latency benchmark on a public dataset (e.g. SIFT1M subset) with an `ef` sweep
- [ ] Scalar quantisation (int8) to cut memory 4x
- [ ] API keys via a bearer-token middleware
