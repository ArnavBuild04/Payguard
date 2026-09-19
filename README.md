<div align="center">

![PayGuard](https://capsule-render.vercel.app/api?type=waving&color=0:4F46E5,50:6366F1,100:06B6D4&height=240&section=header&text=PayGuard&fontSize=72&fontColor=ffffff&animation=fadeIn&fontAlignY=36&desc=Exactly-once%20payments.%20Self-healing%20reconciliation.%20AI-assisted%20judgment.&descAlignY=58&descSize=18&descColor=e0e7ff)

[![Typing SVG](https://readme-typing-svg.demolab.com?font=Fira+Code&weight=600&size=20&duration=2800&pause=900&color=06B6D4&center=true&vCenter=true&multiline=true&repeat=true&width=900&height=80&lines=A+distributed+payments+system+built+around+failure+modes%2C+not+the+happy+path;Four+idempotency+keys.+Two+reconciliation+lanes.+One+deterministic+policy+gate.;LLM+proposes+%E2%80%94+code+and+a+human+both+have+to+agree+before+anything+executes.)](https://git.io/typing-svg)

<br/>

[![Go](https://img.shields.io/badge/Go-1.22-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev)
[![Python](https://img.shields.io/badge/Python-3.12-3776AB?style=for-the-badge&logo=python&logoColor=white)](https://python.org)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=for-the-badge&logo=postgresql&logoColor=white)](https://www.postgresql.org)
[![pgvector](https://img.shields.io/badge/pgvector-0.8-336791?style=for-the-badge&logo=postgresql&logoColor=white)](https://github.com/pgvector/pgvector)
[![Kafka](https://img.shields.io/badge/Redpanda-Kafka_API-EB008B?style=for-the-badge&logo=apachekafka&logoColor=white)](https://redpanda.com)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=for-the-badge&logo=redis&logoColor=white)](https://redis.io)
[![FastAPI](https://img.shields.io/badge/FastAPI-0.115-009688?style=for-the-badge&logo=fastapi&logoColor=white)](https://fastapi.tiangolo.com)
[![LangGraph](https://img.shields.io/badge/LangGraph-0.2-1C3C3C?style=for-the-badge&logo=langchain&logoColor=white)](https://langchain-ai.github.io/langgraph/)
[![Gemini](https://img.shields.io/badge/Gemini-2.5_Flash-8E75B2?style=for-the-badge&logo=googlegemini&logoColor=white)](https://ai.google.dev)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=for-the-badge&logo=docker&logoColor=white)](https://docker.com)

</div>

<br/>

> Most demo payment projects show you the happy path. This one is built around every way that
> path breaks — a late webhook, a duplicated delivery, two requests racing the same idempotency
> key, a provider status nobody's ever seen before — and for each one, there's a specific,
> provable answer for what the system does instead of guessing.

<br/>

## Table of Contents

<table>
<tr><td width="50%" valign="top">

**Understanding the system**
- [The 60-Second Tour](#-the-60-second-tour)
- [The Problem, Properly Stated](#-the-problem-properly-stated)
- [System Architecture](#-system-architecture)
- [Data Model](#-data-model)
- [A Purchase, End to End](#-a-purchase-end-to-end)
- [Payment State Machine](#-payment-state-machine)

</td><td width="50%" valign="top">

**Under the hood**
- [The Idempotency Matrix](#-the-idempotency-matrix)
- [Concurrency Control](#-concurrency-control)
- [Two-Lane Reconciliation](#-two-lane-reconciliation)
- [The Agentic Layer](#-the-agentic-layer)
- [The Trust Boundary](#-the-trust-boundary)
- [RAG over pgvector](#-rag-over-pgvector)

</td></tr>
<tr><td width="50%" valign="top">

**Everything else**
- [Pluggable Everything](#-pluggable-everything)
- [Tech Stack](#-tech-stack)
- [Project Structure](#-project-structure)

</td><td width="50%" valign="top">

**Doing it yourself**
- [Getting Started](#-getting-started)
- [Testing Philosophy](#-testing-philosophy)
- [What's Next](#-whats-next)

</td></tr>
</table>

<br/>

## 🎯 The 60-Second Tour

A client buys something → the core creates a `PROCESSING` payment and calls a payment provider →
the provider confirms asynchronously via webhook → an outbox+Kafka pipeline fans that out to a
coordinator, which grants wallet currency exactly once → and running underneath all of it, a
detector continuously looks for anything that drifted out of sync, fixing the mechanical cases
itself and routing the ambiguous ones to an LLM-assisted human review queue.

```mermaid
flowchart LR
    Client(["🧑 Client"]) -->|"POST /v1/payments"| Core["⚙️ Go Core API"]
    Core -->|create| Provider[("💳 Payment Provider")]
    Core -->|"INSERT PROCESSING"| PaymentsDB[("payments")]
    Provider -.->|"webhook (async)"| Core
    Core -->|"INSERT"| Outbox[("outbox")]
    Outbox -->|"relay · SKIP LOCKED"| Kafka[["📨 Redpanda / Kafka"]]
    Kafka -->|"consume · at-least-once"| Coordinator["🔀 Coordinator"]
    Coordinator -->|grant| Wallet[("💰 Wallet Ledger")]
    Coordinator -->|grant| Assets["🎟️ Asset / Ticket Grant"]

    Detector{{"🔍 Reconciliation Detector"}} -. sweeps .-> PaymentsDB
    Detector -. sweeps .-> Wallet
    Detector -->|"Lane 1 · mechanical"| AutoResolve["🤖 Auto-Resolver"]
    Detector -->|"Lane 2 · ambiguous"| Cases[("reconciliation_cases")]
    Cases --> Agent["🧠 Python Agent<br/>LangGraph + Gemini"]
    Agent -->|"recommendation only"| Cases
    Cases --> Human(["🙋 Human Approver"])
    Human -->|"approve / reject"| PolicyEngine{{"🛡️ Policy Engine"}}
    PolicyEngine -->|permitted| Core

    style Agent fill:#8E75B2,color:#fff,stroke:#333
    style PolicyEngine fill:#dc2626,color:#fff,stroke:#333
    style AutoResolve fill:#16a34a,color:#fff,stroke:#333
    style Core fill:#4F46E5,color:#fff,stroke:#333
```

The whole point of the diagram above: **the LLM sits inside one small box, feeding a queue that
a human and a deterministic gate both still have to clear.** Nothing about "AI" in this system
gets to move money by itself. More on exactly why and how in [The Trust Boundary](#-the-trust-boundary).

<br/>

## 🧩 The Problem, Properly Stated

A payment isn't one event — it's a negotiation between three systems that don't share a
transaction: **our database**, **the provider**, and **the message queue that fans the result out
to grant services.** Any of the following is not a bug, it's Tuesday:

| Failure mode | What actually happens | What handles it |
|---|---|---|
| Webhook never arrives | Provider succeeded; we're still `PROCESSING` forever | `STUCK_PROCESSING` detection after a threshold |
| Webhook arrives twice | Same event delivered by an at-least-once system | `(source, event_id)` unique constraint → idempotent no-op |
| Webhook arrives, then a refund webhook we lost | Provider says `refunded`, we still say `SUCCEEDED` | `LOCAL_AHEAD` → compensate + advance |
| Two requests race the same idempotency key | Client retried a slow request | `(tenant_id, idempotency_key)` unique constraint, fingerprint-checked |
| Kafka redelivers a message | Consumer crashed after processing, before committing offset | `(consumer_group, event_id)` dedup table |
| Grant call fails after payment succeeded | Wallet credit didn't happen, payment says it did | `MISSING_GRANT` detection, redrive via coordinator |
| Provider invents a new status string | Integration hasn't been updated for a provider API change | `UNKNOWN_PROVIDER_STATE` — **always** routed to a human, never guessed |
| Ledger and account balance disagree | A bug, a race, or partial application of an update | `LEDGER_MISMATCH` — flagged, never silently patched |

Every one of these has a name, a detector, and a deterministic answer for what the system does
about it — that mapping *is* the reconciliation layer, and it's the reason this project exists.

<br/>

## 🏢 System Architecture

```mermaid
flowchart TB
    subgraph EXT["External"]
        direction LR
        Client(["Client"])
        Provider[("Payment Provider<br/><i>mocked, real HTTP</i>")]
    end

    subgraph CORE["Go Core — :8080"]
        direction TB
        API["net/http API"]
        Wallet["Wallet Service"]
        Payment["Payment Service"]
        Coord["Coordinator Service"]
        Detector["Reconciliation Detector"]
        Policy["Policy Engine<br/><i>pure functions</i>"]
        OutboxRelay["Outbox Relay"]
        KafkaConsumer["Kafka Consumer"]
        Tools["/internal/tools/*<br/><i>read-only</i>"]
    end

    subgraph DATA["Postgres 16 + pgvector — one database"]
        direction LR
        PG1[("accounts<br/>ledger_entries")]
        PG2[("payments<br/>payment_events")]
        PG3[("transactions<br/>outbox")]
        PG4[("reconciliation_cases")]
        PG5[("agent_runs<br/>policy_chunks")]
    end

    subgraph INFRA["Infra"]
        direction LR
        K[["Redpanda"]]
        R[("Redis<br/>cache + lock")]
    end

    subgraph AGENT["Python Agent — :8090"]
        direction TB
        FastAPI["FastAPI"]
        Graph["LangGraph<br/>4-step flow"]
        RAG["RAG retrieval"]
        LLM{{"Gemini / Fake LLM"}}
    end

    subgraph UI["Approval UI"]
        Page["Static HTML/JS"]
    end

    Client -->|HTTP| API
    API --> Wallet & Payment & Coord
    Payment <-->|HTTP| Provider
    Payment --> PG2
    Wallet --> PG1
    Coord --> PG3
    API --> OutboxRelay
    OutboxRelay -->|SKIP LOCKED| PG3
    OutboxRelay -->|publish| K
    K -->|consume| KafkaConsumer
    KafkaConsumer --> Coord
    OutboxRelay -.leader lock.-> R
    Payment -.read-through cache.-> R

    Detector -->|sweeps| PG1 & PG2 & PG3
    Detector --> PG4
    Detector --> Policy
    API --> Tools
    Tools --> PG2 & PG1 & PG4
    Tools -. "read-only" .-> Provider

    FastAPI --> Graph
    Graph -->|GET only| Tools
    Graph --> RAG
    RAG <--> PG5
    Graph --> LLM
    Graph --> PG5

    Page -->|GET cases + recommendations| API & FastAPI
    Page -->|POST approve/reject| API

    style Policy fill:#dc2626,color:#fff
    style LLM fill:#8E75B2,color:#fff
    style Tools fill:#f59e0b,color:#000
    style Detector fill:#0891b2,color:#fff
```

**Two servers, one database.** The Go core owns every write that touches money or state. The
Python agent reads from Go over plain HTTP GETs and writes to two tables of its own
(`agent_runs`, `policy_chunks`) in the *same* Postgres instance — no separate vector database,
no separate infra to operate.

<br/>

## 📊 Data Model

```mermaid
erDiagram
    PAYMENTS ||--o{ PAYMENT_EVENTS : "receives webhooks"
    PAYMENTS ||--o{ RECONCILIATION_CASES : "may open"
    PAYMENTS ||--o{ TRANSACTIONS : "drives"
    TRANSACTIONS }o--|| ACCOUNTS : "credits / debits"
    ACCOUNTS ||--o{ LEDGER_ENTRIES : "records"
    RECONCILIATION_CASES ||--o{ AGENT_RUNS : "investigated by"

    PAYMENTS {
        bigint id PK
        string tenant_id
        bigint user_id
        string sku
        bigint amount_minor
        string status "PROCESSING|SUCCEEDED|FAILED|REFUND_PENDING|REFUNDED"
        string idempotency_key UK "tenant_id + key"
        string provider_payment_id UK
        int attempts
    }
    PAYMENT_EVENTS {
        bigint id PK
        bigint payment_id FK
        string source
        string event_id UK "source + event_id"
        string event_type
    }
    TRANSACTIONS {
        bigint id PK
        string order_id
        string operation_type UK "order_id + op_type"
        string status "PENDING|SUCCESS|FAILED|REVERSE_PENDING|REVERSED"
        bigint amount_minor
    }
    ACCOUNTS {
        string tenant_id PK
        bigint user_id PK
        bigint balance
    }
    LEDGER_ENTRIES {
        bigint id PK
        string tenant_id
        bigint user_id
        bigint amount
        bigint balance_after
        string reference_id UK "tenant + source + ref"
    }
    RECONCILIATION_CASES {
        bigint id PK
        bigint payment_id FK
        string reason "8 known reasons"
        string status "OPEN|RESOLVED|DISMISSED"
        text evidence_json
    }
    AGENT_RUNS {
        bigint id PK
        bigint case_id FK
        text conclusion
        string confidence "low|medium|high"
        string recommended_action
        text policy_citations_json
    }
    POLICY_CHUNKS {
        bigint id PK
        string doc_name
        int chunk_index
        text content
        vector embedding "768-dim, HNSW cosine index"
    }
```

Two things worth noticing: **`reconciliation_cases` has a partial unique index**
`(payment_id, reason, sub_key) WHERE status = 'OPEN'`, built with raw SQL because GORM's struct
tags can't express a `WHERE` clause on a unique index — it's what stops the detector opening
five duplicate cases for the same problem on every sweep. And **`agent_runs` is append-only** —
a case can be investigated more than once as new evidence arrives, so it's a log, not a 1:1.

<br/>

## 🔄 A Purchase, End to End

```mermaid
sequenceDiagram
    autonumber
    actor C as Client
    participant Core as Go Core
    participant DB as Postgres
    participant P as Provider
    participant OB as Outbox Relay
    participant K as Redpanda
    participant Coord as Coordinator
    participant W as Wallet

    C->>Core: POST /v1/payments {idempotency_key}
    Core->>DB: INSERT payments (PROCESSING)
    Core->>P: CreatePayment()
    Core-->>C: 201 Created

    Note over P,Core: ...some time later, asynchronously...
    P-->>Core: POST /v1/payments/webhooks
    Core->>DB: dedupe on (source, event_id)
    alt already processed
        Core-->>P: 200 OK (idempotent no-op)
    else new event
        Core->>DB: UPDATE payments SET status=SUCCEEDED
        Core->>DB: INSERT outbox (payment.succeeded)
        Core-->>P: 200 OK
    end

    loop every 2s
        OB->>DB: SELECT ... FOR UPDATE SKIP LOCKED
        OB->>K: publish payment.succeeded
        OB->>DB: mark published_at
    end

    K->>Coord: consume (at-least-once delivery)
    Coord->>DB: dedupe on (consumer_group, event_id)
    alt already processed
        Coord->>K: commit offset (skip)
    else new event
        Coord->>W: credit(tenant, user, amount, reference_id)
        W->>DB: SELECT account FOR UPDATE
        W->>DB: INSERT ledger_entries, UPDATE accounts
        W-->>Coord: OK
        Coord->>DB: transactions.status = SUCCESS
        Coord->>K: commit offset
    end
```

If any step here fails and retries, the idempotency key at that specific layer is what makes the
retry a no-op instead of a double-charge or a double-grant. Which key, at which layer, and why —
that's the next section.

<br/>

## 🔀 Payment State Machine

```mermaid
stateDiagram-v2
    [*] --> PROCESSING: POST /v1/payments
    PROCESSING --> SUCCEEDED: provider confirms (webhook or poll)
    PROCESSING --> FAILED: provider confirms failure
    SUCCEEDED --> REFUND_PENDING: reversal detected (webhook or LOCAL_AHEAD case)
    REFUND_PENDING --> REFUNDED: grant clawback complete
    FAILED --> [*]
    REFUNDED --> [*]

    PROCESSING --> PROCESSING: provider 404s, < 2min old (retry)

    note right of PROCESSING
        > 2 min, provider has no record → STUCK_PROCESSING case
        > 5 min, provider keeps erroring → STUCK_PROCESSING case
        unrecognized provider status → UNKNOWN_PROVIDER_STATE case
    end note
```

`CanTransition(from, to)` is a hard-coded map in `payment/models` — every write path, whether
it's a webhook, a poll, or a reconciliation resolution, goes through the same guard. There is no
code path that sets `status` directly without checking it first.

<br/>

## 🔑 The Idempotency Matrix

Four different keys, at four different layers, because four different systems retry for four
different reasons:

| Layer | Key | Retried by | Guard mechanism |
|---|---|---|---|
| **Client request** | `(tenant_id, idempotency_key)` | Client, on timeout/retry | Unique index + fingerprint check: same key + different body ⇒ **rejected**, not silently overwritten |
| **Provider webhook** | `(source, event_id)` | Provider, at-least-once delivery | Unique index on `payment_events`; second delivery is a no-op |
| **Kafka consumer** | `(consumer_group, event_id)` | Kafka, at-least-once by design | `processed_events(consumer_group, event_id)` PK — checked before any side effect |
| **Wallet ledger** | `(tenant_id, source, reference_id)` | A redriven/retried grant | Unique index on `ledger_entries`; a retried credit can't double-apply |

The Postgres translation, used identically at every layer: a `23505` unique-violation on insert
is caught and turned into **"this already happened, return the existing result"** — success, not
an error — while a `40001` serialization failure under `READ COMMITTED` is caught and **retried**,
because it means "someone else touched this row first," not "this is wrong."

<br/>

## 🔧 Concurrency Control

```mermaid
flowchart LR
    subgraph "Wallet credit/debit"
        A["BEGIN"] --> B["SELECT account<br/>FOR UPDATE"]
        B --> C["compute new balance"]
        C --> D["INSERT ledger_entries<br/>UPDATE accounts"]
        D --> E["COMMIT"]
    end
    subgraph "Outbox relay (multi-instance safe)"
        F["SELECT ... FOR UPDATE<br/>SKIP LOCKED"] --> G["publish to Kafka"]
        G --> H["mark published_at"]
    end
```

- **`SELECT ... FOR UPDATE`** at `READ COMMITTED` serializes concurrent debits/credits on the
  *same* account, so two simultaneous purchases by the same user can't read-modify-write the
  balance from a stale snapshot.
- **`SKIP LOCKED`** on the outbox relay means multiple relay instances (or the same instance
  racing itself) can run the sweep concurrently without duplicating a publish — a row already
  locked by another sweep is simply skipped this round, not blocked on or double-sent.
- **Redis leader lock** on the outbox sweep is deliberately **fail-open**: if Redis is down, the
  sweep still runs (money keeps moving), it just loses the extra layer of single-flight
  protection — availability wins over that specific optimization.

<br/>

## 🚦 Two-Lane Reconciliation

The detector finds **eight** named kinds of drift. Two of them are mechanical enough to fix
automatically; the other six always go to a human — and specifically, to a human who's already
read the agent's write-up.

```mermaid
flowchart TD
    D{{"Detector sweep<br/>(every 5s)"}}

    D --> R1["PROVIDER_AHEAD<br/><small>provider terminal, we're not</small>"]
    D --> R2["MISSING_GRANT<br/><small>payment succeeded, grant never landed</small>"]
    D --> R3["LOCAL_AHEAD<br/><small>we're terminal, provider reversed</small>"]
    D --> R4["STUCK_PROCESSING<br/><small>no provider record, or timing out</small>"]
    D --> R5["UNKNOWN_PROVIDER_STATE<br/><small>status string we don't recognize</small>"]
    D --> R6["LEDGER_MISMATCH<br/><small>balance ≠ Σ ledger entries</small>"]
    D --> R7["CLAWBACK_SHORTFALL<br/><small>can't fully recover a reversed grant</small>"]
    D --> R8["UNMATCHED_WEBHOOK<br/><small>webhook for a payment we don't have</small>"]

    R1 --> Gate{"Lane 1 gate:<br/>under ceiling?<br/>attempts left?<br/>no conflicting case?"}
    R2 --> Gate
    Gate -->|pass| Auto["✅ Auto-Resolver<br/>applies the fix, no human"]
    Gate -->|fail| Q

    R3 --> Q["📋 Lane 2 Queue"]
    R4 --> Q
    R5 --> Q
    R6 --> Q
    R7 --> Q
    R8 --> Q

    Q --> Agent["🧠 Agent investigates<br/>(read-only)"]
    Agent --> Rec[["Recommendation +<br/>evidence + policy citations"]]
    Rec --> H{"🙋 Human decides"}
    H -->|Approve| PE{{"🛡️ Policy Engine"}}
    H -->|Reject| Dismiss["DISMISSED<br/>note required"]
    PE -->|permitted| Done["✅ RESOLVED"]
    PE -->|denied — ceiling or<br/>nonsensical action| Blocked["🚫 Blocked, even<br/>if a human clicked Approve"]

    style Auto fill:#16a34a,color:#fff
    style Agent fill:#8E75B2,color:#fff
    style PE fill:#dc2626,color:#fff
    style Blocked fill:#7f1d1d,color:#fff
```

Lane 1's gate is intentionally strict — under the auto-resolve ceiling, attempts remaining,
authoritative evidence, and **no other open case competing for the same payment**. Anything that
fails any one of those checks drops straight to Lane 2. There is no in-between "mostly automated"
tier: a case is either fixed by code with zero human involvement, or it's read by a human who
also sees the agent's reasoning. Nothing resolves on vibes.

<br/>

## 🧠 The Agentic Layer

```mermaid
flowchart LR
    Start(["POST /cases/{id}/investigate"]) --> N1["**1. gather_case**<br/>fetch case + evidence"]
    N1 -->|GET| GC[("Go Core")]
    N1 --> N2["**2. gather_evidence**<br/>cross-check three live sources"]
    N2 -->|GET payment| GC
    N2 -->|"GET provider (live!)"| GC
    N2 -->|GET wallet balance| GC
    N2 --> N3["**3. retrieve_policy**<br/>cosine search over policy docs"]
    N3 -->|top-3 by similarity| PGV[("pgvector<br/>policy_chunks")]
    N3 --> N4["**4. analyze**<br/>structured-output LLM call"]
    N4 -->|"schema-constrained JSON"| LLM{{"Gemini<br/>or FakeLLMClient"}}
    LLM --> Out(["RootCauseAnalysis"])
    Out --> Save[("agent_runs<br/>(append-only)")]

    style N1 fill:#4F46E5,color:#fff
    style N2 fill:#4F46E5,color:#fff
    style N3 fill:#0891b2,color:#fff
    style N4 fill:#8E75B2,color:#fff
    style LLM fill:#8E75B2,color:#fff
```

Four LangGraph nodes, always in this exact order, no branches or loops today. Step 2 is the one
worth dwelling on: the provider lookup is **live**, not the snapshot baked into the case's
evidence at detection time — the whole point is catching a provider status that's changed *since*
the case was opened.

The output is one fixed shape, every time:

```python
class RootCauseAnalysis(BaseModel):
    conclusion: str                        # plain-English theory of what happened
    evidence: list[str]                    # the facts that led there
    confidence: Literal["low", "medium", "high"]
    missing_information: list[str]         # what it couldn't verify — never guessed
    recommended_action: str                # a suggestion. Not a command. See below.
    policy_citations: list[PolicyCitation] # quoted, sourced passages — not remembered ones
```

Notice what's **not** in that schema: no `approved`, no `executed`, no `action_taken`. That
omission is load-bearing — see the next section.

<br/>

## 🔐 The Trust Boundary

```mermaid
flowchart TB
    subgraph SAFE["✅ Can move money"]
        direction LR
        GoCore["Go Core"]
        PolicyEngine{{"policy.Engine<br/><i>pure, deterministic</i>"}}
        Human((("🙋 Human")))
    end

    subgraph UNSAFE["🔒 Read-only + recommend-only"]
        direction LR
        Agent["Python Agent"]
        LLM{{"Gemini"}}
    end

    Agent -->|"4 GET endpoints only"| GoCore
    Agent -->|"writes a suggestion"| AgentRuns[("agent_runs")]
    LLM -. "no client exists for this call" .-> GoCore
    AgentRuns -->|"human reads it"| Human
    Human -->|"POST /approve"| GoCore
    GoCore --> PolicyEngine
    PolicyEngine -->|"permitted (reason, action, amount)?"| Execute[["💸 Execute resolution"]]
    PolicyEngine -.->|"denied, even if human clicked Approve"| Blocked["🚫 Rejected"]

    style PolicyEngine fill:#dc2626,color:#fff
    style Agent fill:#8E75B2,color:#fff
    style LLM fill:#8E75B2,color:#fff
    style Execute fill:#16a34a,color:#fff
    style Blocked fill:#7f1d1d,color:#fff
```

Three **independent** layers, not one:

1. **Structural** — `CoreToolsClient`, the agent's only channel to the Go core, has exactly four
   methods, and all four are HTTP `GET`. There is no function in the entire Python codebase that
   *could* call `/approve` even if a prompt tried to talk it into doing so.
2. **Schema** — `RootCauseAnalysis` has no field that means "do this." `recommended_action` is a
   string a human reads on a web page; nothing parses it and executes it automatically.
3. **Policy** — even when a human clicks Approve with the agent's exact suggestion, Go's
   `policy.Engine.Evaluate(reason, action, amount)` independently re-checks it against a
   hard-coded permitted-actions table and a compensation ceiling. Same inputs, same answer,
   every time — no model, no variance, no exceptions.

```go
// internal/policy/engine.go — the whole safety net, in one pure function
func (e *Engine) Evaluate(reason models.Reason, action models.Action, amountMinor int64) Decision {
    allowed, ok := permittedActions[reason]
    if !ok || !allowed[action] {
        return Decision{Permitted: false, Reason: "not a permitted resolution for this reason"}
    }
    if action == models.ActionCompensate && amountMinor > e.maxCompensateMinor {
        return Decision{Permitted: false, Reason: "compensate amount exceeds the policy ceiling"}
    }
    return Decision{Permitted: true}
}
```

<br/>

## 📚 RAG over pgvector

```mermaid
flowchart TB
    subgraph Ingest["Ingest (offline, once per policy edit)"]
        Doc["policy .md files"] --> Chunk["chunk by paragraph<br/>≤ 500 chars"]
        Chunk --> Embed1["embed each chunk"]
        Embed1 --> Store[("policy_chunks<br/>vector(768) + HNSW index")]
    end
    subgraph Retrieve["Retrieve (every investigation)"]
        Query["case reason + evidence"] --> Embed2["embed the query"]
        Embed2 --> Search["ORDER BY<br/>embedding <=> query<br/>LIMIT 3"]
        Store --> Search
        Search --> Cited["cited passages<br/>with similarity score"]
    end
```

Three short, plain-English policy documents (`refund_policy.md`, `clawback_policy.md`,
`provider_status_policy.md`) are chunked, embedded, and stored as native `vector(768)` columns in
the **same** Postgres the Go core already runs — no dedicated vector database, because a few
dozen chunks doesn't come close to earning that operational overhead. Retrieval is cosine
similarity (`<=>` operator) over an HNSW index, ranked and limited entirely inside Postgres.

Why this matters over just trusting the model's training data: the LLM can only cite what's
*actually written* in this company's policy docs, quoted verbatim with a similarity score — not
what it half-remembers about refund policy in general.

<br/>

## 🔌 Pluggable Everything

Every external dependency in this system — a payment gateway, a grant service, an LLM, an
embedding model — has a real implementation and a deterministic fake behind the same interface.
The rest of the code never knows or cares which one it's holding.

| Interface | Real implementation | Deterministic fake | Why it matters |
|---|---|---|---|
| `provider.Provider` | `HTTPProvider` → mock PSP over HTTP | `SimulatedProvider` (in-memory) | Every Go test runs without a live payment gateway |
| `coordinatorgranter.TicketGranter` | `HTTPTicketGranter` | `InMemoryGranter` | Grant-failure scenarios are constructed, not waited for |
| `llm.LLMClient` | `GeminiLLMClient` (structured output) | `FakeLLMClient` (rule-based) | The whole agent runs with **zero API key, zero cost** |
| `rag.Embedder` | `GeminiEmbedder` (`text-embedding-004`) | `FakeEmbedder` (hashed shingles, unit-normalized) | RAG retrieval is fully tested offline |

**Practical result:** `GEMINI_API_KEY` unset is not a degraded mode — it's the default,
documented, fully-tested state. Clone the repo, run the tests, run the demo, all without signing
up for anything.

<br/>

## 🧰 Tech Stack

<div align="center">

| Layer | Choice | Why |
|---|---|---|
| Core language | **Go** | Static typing, cheap concurrency, one static binary per service |
| ORM | **GORM** over Postgres | Struct-tag migrations for the common case, raw SQL for the partial index GORM can't express |
| Message bus | **Redpanda** (Kafka API) | One container instead of Kafka's usual two (no separate ZooKeeper) |
| Cache / lock | **Redis** | Read-through cache + a fail-open leader lock, nothing load-bearing depends on it being up |
| Agent framework | **FastAPI + LangGraph** | Explicit, inspectable 4-node flow; a typed HTTP surface with 4 routes, no more |
| LLM | **Google Gemini** (structured output) | Generous free tier + first-class JSON-schema-constrained generation |
| Vector search | **pgvector** in the existing Postgres | A few dozen chunks doesn't justify a dedicated vector DB |
| Approval UI | **Static HTML/JS**, zero build step | One page, four fetch calls, nothing to compile |
| Infra | **Docker Compose** | `docker compose up -d` and the whole backing stack is live |

</div>

<br/>

## 📂 Project Structure

<details>
<summary><b>Click to expand the full tree</b></summary>

```
payguard/
├── core/                          Go service
│   ├── cmd/
│   │   ├── server/                 the real app — :8080
│   │   ├── mockprovider/            always-honest fake PSP — :9090
│   │   └── grantstub/                fake asset/ticket grant service — :9100
│   └── internal/
│       ├── wallet/                  accounts, ledger_entries
│       ├── payment/                  payments, payment_events
│       ├── coordinator/               transactions, grant orchestration
│       ├── provider/                   real + simulated payment gateway client
│       ├── outbox/                      transactional outbox relay
│       ├── kafka/                        producer, consumer, DLQ routing
│       ├── cache/                         Redis wrapper
│       ├── reconcile/
│       │   ├── detector/                  8-reason drift detection
│       │   ├── service/                    resolve(), auto-resolver, Lane 1 gate
│       │   └── httpapi/                     /v1/reconciliation-cases/*
│       ├── policy/                       deterministic permitted-action engine
│       ├── tools/httpapi/                 /internal/tools/* — agent's read-only surface
│       └── unmatchedwebhook/               orphaned webhook capture
│
├── agent/                          Python service
│   ├── app/
│   │   ├── main.py                    FastAPI — 4 routes total
│   │   ├── agent_graph.py              the LangGraph 4-step flow
│   │   ├── investigate.py               wires deps, runs the flow, persists
│   │   ├── tools_client.py               the agent's only Go client — 4 GET methods
│   │   ├── schemas.py                     RootCauseAnalysis and friends
│   │   ├── agent_runs_repo.py              save / read recommendations
│   │   ├── llm/
│   │   │   ├── gemini_client.py               real, structured-output Gemini call
│   │   │   └── fake_client.py                  deterministic rule-based stand-in
│   │   ├── rag/
│   │   │   ├── embeddings.py                   real + fake embedders
│   │   │   ├── ingest.py                        chunk → embed → store
│   │   │   └── retrieve.py                       cosine search over pgvector
│   │   └── policies/                     the 3 source-of-truth policy documents
│   ├── migrations/                     agent_runs + policy_chunks DDL
│   ├── tests/                          9 tests, zero network calls required
│   └── ui/
│       └── index.html                    the entire approval UI, one file
│
└── docker-compose.yml               postgres (pgvector), redpanda, redis
```

</details>

<br/>

## 🚀 Getting Started

<details open>
<summary><b>1. Infrastructure</b></summary>

```bash
docker compose up -d       # postgres (with pgvector), redpanda, redis
```

</details>

<details>
<summary><b>2. Go core + its fakes</b></summary>

```bash
cd payguard/core
cp config.example.json config.json      # defaults work as-is locally
go run ./cmd/mockprovider &              # fake payment gateway  — :9090
go run ./cmd/grantstub &                  # fake grant service    — :9100
go run ./cmd/server &                      # the core API           — :8080
```

</details>

<details>
<summary><b>3. Python agent</b></summary>

```bash
cd payguard/agent
python3.12 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt

python -m app.migrate          # idempotent — creates agent_runs + policy_chunks
python -m app.rag.ingest       # chunk + embed the policy docs (fake embedder if no key set)
uvicorn app.main:app --port 8090 &
```

Leaving `GEMINI_API_KEY` unset in `.env` runs the whole pipeline — evidence gathering, RAG
retrieval, structured recommendation, persistence — on deterministic fakes. No key, no cost, no
external call, same code path.

</details>

<details>
<summary><b>4. The approval UI</b></summary>

```bash
cd payguard/agent/ui
python3 -m http.server 8091
# open http://localhost:8091
```

</details>

<br/>

## 🧪 Testing Philosophy

```bash
cd payguard/core  && go test -race ./...           # unit + integration, real local Postgres
cd payguard/agent && python -m pytest tests/ -v     # unit + integration, zero network calls
```

Both suites test the same principle from opposite ends: **never test whether the AI's answer is
subjectively good — test whether the wiring around it is provably correct.** The fake LLM lets
every structural guarantee get asserted directly and deterministically:

```python
def test_never_produces_an_approve_or_execute_field():
    """A regression here would mean the schema itself could carry an authorization —
    checked directly against the model's fields, not inferred from behavior."""
    fields = set(RootCauseAnalysis.model_fields.keys())
    assert "approved" not in fields
    assert "executed" not in fields
    assert "action_taken" not in fields
```

Integration tests on both sides of the language boundary follow the same convention: skip
automatically if their live dependency (a real Postgres, a running Go core) isn't up, rather than
failing the whole suite in an environment that hasn't started infra yet.

<br/>

## 🧭 What's Next

- **Streaming investigations** — show a human the agent's reasoning as each of the 4 steps
  completes, instead of waiting for the full round trip.
- **A labeled eval set** — `agent_runs` is already an append-only log of every recommendation
  ever made; pairing it with human approve/reject outcomes is the raw material for tracking
  whether the model's recommendations are actually getting better or drifting over time.
- **Semantic chunking** for the policy corpus once it grows past a handful of documents —
  fixed-paragraph packing is a reasonable start at 3 documents, not at 300.

<br/>

<div align="center">

![footer](https://capsule-render.vercel.app/api?type=waving&color=0:06B6D4,100:4F46E5&height=140&section=footer)

</div>
