CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE payment_status AS ENUM (
    'pending',
    'processing',
    'completed',
    'failed',
    'rejected'
);

CREATE TYPE ledger_entry_type AS ENUM (
    'debit',
    'credit',
    'refund'
);

CREATE TYPE ledger_direction AS ENUM (
    'debit',
    'credit'
);

CREATE TABLE accounts (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE payments (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency VARCHAR(10) NOT NULL,
    sender_id UUID NOT NULL REFERENCES accounts(id),
    receiver_id UUID NOT NULL REFERENCES accounts(id),
    idempotency_key VARCHAR(100) UNIQUE NOT NULL,
    status payment_status NOT NULL DEFAULT 'pending',
    error_code VARCHAR(50),
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (sender_id <> receiver_id)
);

CREATE TABLE ledger_entries (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    payment_id UUID NOT NULL REFERENCES payments(id),
    account_id UUID NOT NULL REFERENCES accounts(id),
    type ledger_entry_type NOT NULL,
    direction ledger_direction NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency VARCHAR(10) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(payment_id, account_id, type),

    CHECK (
        (type = 'debit' AND direction = 'debit') OR
        (type = 'credit' AND direction = 'credit') OR
        (type = 'refund' AND direction = 'credit')
    )
);

CREATE TABLE payment_events (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    payment_id UUID NOT NULL REFERENCES payments(id),
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX payments_sender_id_idx ON payments(sender_id);
CREATE INDEX payments_receiver_id_idx ON payments(receiver_id);
CREATE INDEX payments_status_idx ON payments(status);

CREATE INDEX ledger_entries_account_id_created_at_idx
ON ledger_entries(account_id, created_at);

CREATE INDEX ledger_entries_payment_id_idx
ON ledger_entries(payment_id);

CREATE INDEX payment_events_payment_id_created_at_idx
ON payment_events(payment_id, created_at);
