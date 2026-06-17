CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS wallets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    withdrawal_id UUID NULL,
    amount BIGINT NOT NULL CHECK (amount <> 0),
    kind TEXT NOT NULL CHECK (kind IN ('DEPOSIT', 'WITHDRAWAL_DEBIT', 'WITHDRAWAL_REVERSAL')),
    idempotency_key UUID NULL,
    bank_reference TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_created
    ON ledger_entries(wallet_id, created_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ledger_entries_withdrawal_kind
    ON ledger_entries(withdrawal_id, kind)
    WHERE withdrawal_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS scheduled_withdrawals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    execute_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'SCHEDULED' CHECK (
        status IN (
            'SCHEDULED',
            'PROCESSING',
            'SUCCEEDED',
            'FAILED_INSUFFICIENT_FUNDS',
            'FAILED_BANK_REJECTED',
            'REQUIRES_RECONCILIATION'
        )
    ),
    idempotency_key UUID NOT NULL DEFAULT gen_random_uuid(),
    bank_reference TEXT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NULL,
    processing_started_at TIMESTAMPTZ NULL,
    processed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_scheduled_withdrawals_due
    ON scheduled_withdrawals(execute_at, id)
    WHERE status = 'SCHEDULED';

CREATE INDEX IF NOT EXISTS idx_scheduled_withdrawals_reconcile
    ON scheduled_withdrawals(updated_at, id)
    WHERE status = 'REQUIRES_RECONCILIATION';

CREATE INDEX IF NOT EXISTS idx_scheduled_withdrawals_processing
    ON scheduled_withdrawals(processing_started_at, id)
    WHERE status = 'PROCESSING';
