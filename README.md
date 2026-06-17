# Chronos Wallet

Chronos Wallet is a Go implementation of the scheduled wallet interview task. It supports synchronous deposits and future-dated withdrawals whose balances are validated at execution time, not at scheduling time.

The implementation uses:

- Go, `chi`, and `pgx`
- PostgreSQL as both source of truth and worker queue
- Append-only signed ledger entries
- Wallet row locks for per-wallet consistency
- `FOR UPDATE SKIP LOCKED` for concurrent withdrawal workers
- A mock bank client with a configurable failure probability

## Run Locally

Start Postgres and the API:

```bash
docker compose up --build
```

The API listens on:

```text
http://localhost:8080
```

Swagger UI is available at:

```text
http://localhost:8080/swagger/
```

The raw OpenAPI document is available at:

```text
http://localhost:8080/swagger/openapi.yaml
```

## Run Without Docker

Start a local PostgreSQL database and set:

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/chronos_wallet?sslmode=disable"
go run ./cmd/server
```

The service runs migrations automatically on startup.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP listen address |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/chronos_wallet?sslmode=disable` | PostgreSQL connection string |
| `WORKER_COUNT` | `4` | Number of withdrawal execution workers |
| `WORKER_POLL_INTERVAL` | `1s` | Due-withdrawal polling interval |
| `RECONCILE_INTERVAL` | `10s` | Reconciliation polling interval |
| `PROCESSING_TIMEOUT` | `2m` | Time after which stuck processing rows are rescheduled |
| `BANK_TIMEOUT` | `3s` | Mock bank request timeout |
| `BANK_MOCK_FAILURE_RATE` | `0.10` | Explicit bank rejection probability |
| `BANK_MOCK_NETWORK_ERROR_RATE` | `0.0` | Unknown/network failure probability |

## API Flow

Create a wallet:

```bash
curl -s -X POST http://localhost:8080/wallets
```

Deposit funds using minor units:

```bash
curl -s -X POST http://localhost:8080/wallets/{wallet_id}/deposits \
  -H 'Content-Type: application/json' \
  -d '{"amount":10000}'
```

Schedule a future withdrawal:

```bash
curl -s -X POST http://localhost:8080/wallets/{wallet_id}/withdrawals \
  -H 'Content-Type: application/json' \
  -d '{"amount":2500,"execute_at":"2030-01-01T12:30:00Z"}'
```

Check withdrawal status:

```bash
curl -s http://localhost:8080/withdrawals/{withdrawal_id}
```

Check balance:

```bash
curl -s http://localhost:8080/wallets/{wallet_id}/balance
```

## Withdrawal Statuses

- `SCHEDULED`: waiting for `execute_at`
- `PROCESSING`: claimed by a worker
- `SUCCEEDED`: bank transfer succeeded
- `FAILED_INSUFFICIENT_FUNDS`: execution-time balance check failed
- `FAILED_BANK_REJECTED`: bank explicitly rejected the transfer
- `REQUIRES_RECONCILIATION`: bank result is unknown and needs status lookup

## Consistency Model

Balances are calculated from signed ledger entries:

- deposits append positive entries
- withdrawal execution appends a negative debit after locking the wallet row
- rejected bank transfers append a positive reversal

This keeps wallet history auditable and avoids mutable balance drift. Concurrent changes on the same wallet serialize through `SELECT ... FOR UPDATE` on the wallet row.

## Test

```bash
go test ./...
```
