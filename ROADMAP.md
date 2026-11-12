# Roadmap

- [ ] Write-ahead log so writes between snapshots survive a crash
- [x] Compaction: rebuild a collection's graph when soft-deleted nodes pass a threshold
- [x] Range filters on numeric metadata (`{"year": {"gte": 2020}}`)
- [x] Go client package (`client/`)
- [x] Small Python client (`clients/python`, standard library only)
- [x] Prometheus metrics endpoint (request latency, collection sizes)
- [x] Recall/latency `ef` sweep tool (`cmd/vecbench`, synthetic clustered data)
- [ ] Run the sweep on a public dataset (e.g. SIFT1M subset)
- [ ] Scalar quantisation (int8) to cut memory 4x
- [x] API keys via a bearer-token middleware
