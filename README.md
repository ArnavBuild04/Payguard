# PayGuard

A payments reconciliation system for a game store: purchase → provider confirmation → wallet
grant, **exactly once**, with an automated reconciler for every way that guarantee can break —
plus an LLM-assisted investigation layer for the discrepancies that need a human's judgment.

Built to be read, not just run: every non-obvious decision below is a decision made under a
specific, named failure mode, not a default.

```
Client ──► Go core (Postgres, Redpanda/Kafka, Redis) ──► Provider (payment gateway, mocked)
                    │
                    ├─ Outbox → Kafka → grant service (wallet credit / asset / ticket)
                    ├─ Deterministic reconciliation detector + auto-resolver (Lane 1)
                    └─ Lane 2 cases ──► Python agent (FastAPI + LangGraph + Gemini + pgvector RAG)
                                              │
                                              └─► recommendation only — a human approves,
                                                  and a separate deterministic policy engine
                                                  gates every resolution regardless of source
```

## Why this exists

Payment systems are distributed systems with money on the line: the provider's webhook can
arrive late, twice, or never; a process can crash mid-transaction; two requests can race for the
same idempotency key. Most demo payment projects assume the happy path. This one is built around
the failure paths — every mechanism here exists because a specific inconsistency is possible and
needs a specific, provable answer for what happens instead.

## What's actually interesting here

- **Idempotency at every layer, for a different reason at each one.** Client-facing requests are
  deduplicated by `(tenant_id, idempotency_key)` with fingerprint checking (same key, different
  body → rejected, not silently overwritten). Provider webhooks are deduplicated by
  `(source, event_id)` because providers retry deliveries. Kafka consumers are deduplicated by
  `(consumer_group, event_id)` because Kafka is at-least-once by design. Wallet credits/debits
  are deduplicated by `(tenant_id, source, reference_id)` because a retried grant must not double-
  credit. Four different idempotency keys, four different failure modes, on purpose.
- **Concurrency correctness, not just correctness.** Wallet balance updates use
  `SELECT ... FOR UPDATE` at `READ COMMITTED` to serialize concurrent debits/credits on the same
  account; the outbox relay uses `SKIP LOCKED` so multiple relay instances can run without
  duplicating work; Postgres unique-constraint violations (`23505`) are turned into idempotency
  hits rather than errors, and transient serialization failures (`40001`) are retried, not swallowed.
- **A two-lane reconciliation model.** A deterministic detector sweeps for every known type of
  drift (provider-ahead, local-ahead, missing grant, stuck processing, unknown provider status,
  ledger mismatch, clawback shortfall, unmatched webhook). The textbook cases — provider is
  authoritative and the fix is mechanical — auto-resolve in Go with **no AI involved**. The
  genuinely ambiguous cases open a human-approval queue instead of guessing.
- **An LLM layer with a hard authority boundary.** The Python agent that investigates ambiguous
  cases can only *read* (four HTTP GET endpoints into the Go core) and can only *write a
  recommendation* (one row in an `agent_runs` table). It has no code path capable of calling the
  approval endpoint. Whatever it recommends still has to clear a deterministic Go policy engine
  before anything executes — the model proposes, code and a human both dispose. See
  `AGENTIC_LAYER_GUIDE.md` (gitignored — personal notes) for the full design writeup, or read on
  below for the summary.
- **Everything is pluggable and tested without external dependencies.** The payment provider has
  a real HTTP implementation and a deterministic in-memory fake used throughout the test suite.
  The LLM client and the embedding model follow the exact same pattern — a real Gemini
  implementation and a deterministic fake, selected by whether `GEMINI_API_KEY` is set. The full
  system, RAG retrieval included, runs and is fully tested with zero API keys and zero network
  calls to third parties.

## Architecture

| Service | Tech | Owns | Talks to |
|---|---|---|---|
| Wallet | Go / GORM / Postgres | `accounts`, `ledger_entries` | Postgres |
| Payment | Go / GORM / Postgres | `payments`, `payment_events` | Provider (HTTP), outbox |
| Coordinator | Go | `transactions` | Wallet, asset/ticket grant services |
| Outbox relay | Go | `outbox` | Postgres, Kafka (producer) |
| Kafka consumer | Go / segmentio/kafka-go | `processed_events` | Redpanda |
| Reconciler | Go | `reconciliation_cases` | Provider (read-only), Payment, Coordinator |
| Policy engine | Go (pure functions) | — | Reconciler only |
| Agent | Python / FastAPI / LangGraph | `agent_runs`, `policy_chunks` (pgvector) | Go core (read-only), Gemini |
| Approval UI | Static HTML/JS | — | Go core, Agent |

Infra: Postgres 16 with the `pgvector` extension (one database, shared by the Go core and the
Python agent — no separate vector store), Redpanda (Kafka-API-compatible), Redis (cache + a
fail-open leader lock for the outbox sweep).

## Running it

```bash
docker compose up -d                     # postgres (pgvector), redpanda, redis

cd payguard/core
cp config.example.json config.json       # fill in if needed; defaults work locally
go run ./cmd/mockprovider &              # fake payment gateway
go run ./cmd/grantstub &                 # fake asset/ticket grant service
go run ./cmd/server &                    # the core API, :8080

cd ../agent
python3.12 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
python -m app.migrate                    # creates agent_runs + policy_chunks
python -m app.rag.ingest                 # embeds the policy docs (fake embedder if no API key)
uvicorn app.main:app --port 8090 &       # the agent API, :8090

cd ui && python3 -m http.server 8091     # approval UI at http://localhost:8091
```

Leaving `GEMINI_API_KEY` unset runs the agent on a deterministic fake LLM and fake embeddings —
the full pipeline (evidence gathering, RAG retrieval, structured recommendation, persistence)
still runs exactly as it would with a real key, just without the network call.

## Testing

```bash
cd payguard/core && go test -race ./...                 # Go: unit + integration against a real local Postgres
cd payguard/agent && python -m pytest tests/ -v          # Python: unit + integration, zero network calls
```

Both suites include integration tests that talk to a real, running Postgres and (for the agent's
end-to-end test) a real, running Go core — skipped automatically if that dependency isn't up,
the same convention on both sides of the language boundary.

## Project layout

```
payguard/
  core/            Go service — payments, wallet, coordinator, Kafka, reconciliation, policy
  agent/           Python service — LangGraph investigation flow, pgvector RAG, FastAPI
    app/policies/  The three short policy documents the RAG layer retrieves from
    ui/            Static human-approval page
docker-compose.yml Postgres (pgvector), Redpanda, Redis
```
