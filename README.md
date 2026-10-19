# vecstore

[![CI](https://github.com/Amineabdessalem/vecstore/actions/workflows/ci.yml/badge.svg)](https://github.com/Amineabdessalem/vecstore/actions/workflows/ci.yml)

A small vector database written in Go with no dependencies outside the
standard library. It stores embeddings in named collections, answers
nearest-neighbour queries with an HNSW graph index, filters by metadata, and
persists to disk with atomic snapshots. It ships as a single static binary
and a distroless Docker image.

## Features

- **HNSW index** (Hierarchical Navigable Small World graphs) with the
  neighbour-diversity heuristic, tunable `M`, `efConstruction` and `efSearch`
- **Cosine, Euclidean and dot-product** distance
- **Collections** with per-vector JSON metadata and **filtered search**
  (equality, ranges and set membership)
- **JSON HTTP API** with batch upserts, get/delete by id and per-query `ef`
- **Snapshots**: periodic and on-shutdown, written atomically (temp file + rename)
- **Exact flat index** used as ground truth to measure HNSW recall in tests

## Performance

Measured by the test suite and benchmark on 4 CPU cores:

| Dataset | Metric | Recall@10 vs exact | Search latency (k=10) |
|---|---|---:|---:|
| 2,000 × 32-d random | cosine | 0.996 | |
| 2,000 × 32-d random | euclidean | 0.994 | |
| 10,000 × 64-d random | cosine | | ~0.27 ms/query |

Defaults: `M=16`, `efConstruction=200`, `efSearch=64`.

## Run it

```bash
# From source
go run ./cmd/vecstore -addr :8080 -data ./data

# With Docker
docker build -t vecstore .
docker run -p 8080:8080 -v vecstore-data:/data vecstore
```

Flags: `-addr`, `-data` (snapshot directory; empty disables persistence),
`-snapshot-every` (default `5m`), `-m`, `-ef-construction`, `-ef-search`.

## API

```bash
# Create a collection (metric defaults to cosine)
curl -XPOST localhost:8080/collections -d '{"name":"docs","dim":3,"metric":"cosine"}'

# Upsert vectors with metadata
curl -XPUT localhost:8080/collections/docs/vectors -d '{
  "vectors": [
    {"id": "go",     "values": [0.9, 0.1, 0.0], "metadata": {"lang": "go"}},
    {"id": "rust",   "values": [0.8, 0.2, 0.1], "metadata": {"lang": "rust"}},
    {"id": "python", "values": [0.1, 0.9, 0.2], "metadata": {"lang": "python"}}
  ]
}'

# k-NN search, optionally filtered and with a custom ef
curl -XPOST localhost:8080/collections/docs/search -d '{"vector":[1,0,0],"k":2}'
curl -XPOST localhost:8080/collections/docs/search -d '{"vector":[1,0,0],"k":2,"filter":{"lang":"rust"},"ef":128}'
```

Filters combine conditions with AND. A plain value means equality; an object
applies operators `eq`, `ne`, `gt`, `gte`, `lt`, `lte` and `in`:

```json
{"filter": {"lang": {"in": ["go", "rust"]}, "year": {"gte": 2020, "lt": 2025}}}
```

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Liveness check |
| `GET` | `/collections` | List collections |
| `POST` | `/collections` | Create `{name, dim, metric}` |
| `GET` | `/collections/{name}` | Collection info and count |
| `DELETE` | `/collections/{name}` | Drop a collection |
| `PUT` | `/collections/{name}/vectors` | Batch upsert `{vectors: [{id, values, metadata}]}` |
| `GET` | `/collections/{name}/vectors/{id}` | Fetch a vector |
| `DELETE` | `/collections/{name}/vectors/{id}` | Delete a vector |
| `POST` | `/collections/{name}/search` | `{vector, k, ef?, filter?}` → `{hits: [{id, distance, metadata}]}` |

## Layout

```
index/    distance metrics, exact Flat index, HNSW graph
store/    collections, metadata filters, snapshots
server/   HTTP handlers
cmd/vecstore/  server binary
```

## How HNSW works here

Each vector is assigned a random top layer (exponentially distributed).
Inserts greedily descend from the entry point to that layer, then run a beam
search of width `efConstruction` on each lower layer and connect to up to `M`
neighbours (`2M` on layer 0), choosing neighbours that are closer to the new
node than to each other so the graph keeps long-range links. Queries descend
the same way and run a beam search of width `max(ef, k)` on layer 0. Deletes
are soft: the node stays in the graph for routing but is never returned.

## Roadmap

See [ROADMAP.md](ROADMAP.md).

## License

MIT
