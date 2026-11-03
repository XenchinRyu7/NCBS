-- ============================================================================
-- Nusantara Core Banking System (CBS) — Production Database Schema (PG15+)
-- Standard: Enterprise Double-Entry Core Ledger & SNAP-BI Open Banking
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Enumerated Types
DO $$ BEGIN
    CREATE TYPE user_role AS ENUM ('CUSTOMER', 'MERCHANT', 'TELLER', 'CUSTOMER_SERVICE', 'AUDITOR', 'ADMIN');
EXCEPTION WHEN duplicate_object THEN null; END $$;

DO $$ BEGIN
    CREATE TYPE kyc_status AS ENUM ('UNVERIFIED', 'VERIFIED', 'REJECTED', 'ENHANCED_DUE_DILIGENCE');
EXCEPTION WHEN duplicate_object THEN null; END $$;

DO $$ BEGIN
    CREATE TYPE account_type AS ENUM ('SAVINGS', 'SAVINGS_YOUTH', 'CHECKING', 'TIME_DEPOSIT', 'GL_INTERNAL');
EXCEPTION WHEN duplicate_object THEN null; END $$;

DO $$ BEGIN
    CREATE TYPE card_tier AS ENUM ('SILVER', 'GOLD', 'PLATINUM', 'PRIORITY');
EXCEPTION WHEN duplicate_object THEN null; END $$;

DO $$ BEGIN
    CREATE TYPE account_status AS ENUM ('ACTIVE', 'DORMANT', 'FROZEN', 'PIN_BLOCKED', 'CLOSED');
EXCEPTION WHEN duplicate_object THEN null; END $$;

DO $$ BEGIN
    CREATE TYPE tx_channel AS ENUM ('INTRABANK', 'BI_FAST', 'RTGS', 'LLG', 'VIRTUAL_ACCOUNT', 'TELLER_DEPOSIT', 'TELLER_WITHDRAWAL');
EXCEPTION WHEN duplicate_object THEN null; END $$;

DO $$ BEGIN
    CREATE TYPE tx_status AS ENUM ('PENDING', 'SETTLED', 'FAILED', 'REVERSED');
EXCEPTION WHEN duplicate_object THEN null; END $$;

DO $$ BEGIN
    CREATE TYPE entry_direction AS ENUM ('DEBIT', 'CREDIT');
EXCEPTION WHEN duplicate_object THEN null; END $$;

DO $$ BEGIN
    CREATE TYPE va_status AS ENUM ('OPEN', 'PAID', 'EXPIRED', 'CANCELLED');
EXCEPTION WHEN duplicate_object THEN null; END $$;

DO $$ BEGIN
    CREATE TYPE audit_action AS ENUM ('CREATE', 'UPDATE', 'DELETE', 'AUTHENTICATE', 'TRANSFER', 'VA_PAYMENT', 'PIN_FAILURE', 'PIN_UNBLOCK');
EXCEPTION WHEN duplicate_object THEN null; END $$;

-- 1. Customers / CIF Table
CREATE TABLE IF NOT EXISTS customers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    cif_number VARCHAR(20) UNIQUE NOT NULL,
    nik_encrypted VARCHAR(255) NOT NULL,
    full_name VARCHAR(150) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    phone_number VARCHAR(30) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role user_role DEFAULT 'CUSTOMER' NOT NULL,
    kyc_status kyc_status DEFAULT 'VERIFIED' NOT NULL,
    is_active BOOLEAN DEFAULT TRUE NOT NULL,
    created_at TIMESTAMP WITH TIMEZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at TIMESTAMP WITH TIMEZONE DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- 2. Accounts Table (Rekening Nasabah & General Ledger Internal)
CREATE TABLE IF NOT EXISTS accounts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    account_number VARCHAR(16) UNIQUE NOT NULL,
    customer_id UUID REFERENCES customers(id) ON DELETE RESTRICT NOT NULL,
    account_type account_type DEFAULT 'SAVINGS' NOT NULL,
    card_tier card_tier DEFAULT 'PLATINUM' NOT NULL,
    currency VARCHAR(3) DEFAULT 'IDR' NOT NULL,
    balance BIGINT DEFAULT 0 NOT NULL CHECK (balance >= 0),
    hold_balance BIGINT DEFAULT 0 NOT NULL CHECK (hold_balance >= 0),
    daily_limit BIGINT DEFAULT 250000000 NOT NULL,
    pin_hash VARCHAR(255) NOT NULL,
    pin_failed_attempts INT DEFAULT 0 NOT NULL,
    status account_status DEFAULT 'ACTIVE' NOT NULL,
    version BIGINT DEFAULT 1 NOT NULL,
    created_at TIMESTAMP WITH TIMEZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at TIMESTAMP WITH TIMEZONE DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- 3. Transactions Header Table
CREATE TABLE IF NOT EXISTS transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    reference_no VARCHAR(40) UNIQUE NOT NULL,
    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    source_account_id UUID REFERENCES accounts(id),
    destination_account_id UUID REFERENCES accounts(id),
    external_bank_code VARCHAR(20),
    external_account_no VARCHAR(30),
    external_account_name VARCHAR(150),
    channel tx_channel NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    fee_amount BIGINT DEFAULT 0 NOT NULL CHECK (fee_amount >= 0),
    total_debit BIGINT NOT NULL CHECK (total_debit > 0),
    currency VARCHAR(3) DEFAULT 'IDR' NOT NULL,
    description TEXT,
    status tx_status DEFAULT 'SETTLED' NOT NULL,
    initiated_by UUID REFERENCES customers(id),
    client_ip VARCHAR(45) NOT NULL,
    trace_id VARCHAR(100) NOT NULL,
    settled_at TIMESTAMP WITH TIMEZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_at TIMESTAMP WITH TIMEZONE DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- 4. Double-Entry Immutable Ledger Postings
CREATE TABLE IF NOT EXISTS ledger_entries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    transaction_id UUID REFERENCES transactions(id) ON DELETE RESTRICT NOT NULL,
    account_id UUID REFERENCES accounts(id) ON DELETE RESTRICT NOT NULL,
    direction entry_direction NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    balance_before BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    narration VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIMEZONE DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- 5. Virtual Accounts Table
CREATE TABLE IF NOT EXISTS virtual_accounts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    va_number VARCHAR(25) UNIQUE NOT NULL,
    merchant_name VARCHAR(150) NOT NULL,
    merchant_account_id UUID REFERENCES accounts(id) NOT NULL,
    customer_name VARCHAR(150) NOT NULL,
    bill_amount BIGINT NOT NULL CHECK (bill_amount > 0),
    currency VARCHAR(3) DEFAULT 'IDR' NOT NULL,
    description VARCHAR(255),
    status va_status DEFAULT 'OPEN' NOT NULL,
    paid_transaction_id UUID REFERENCES transactions(id),
    expires_at TIMESTAMP WITH TIMEZONE NOT NULL,
    paid_at TIMESTAMP WITH TIMEZONE,
    created_at TIMESTAMP WITH TIMEZONE DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- 6. Open Banking Sandbox API Keys
CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    partner_name VARCHAR(100) NOT NULL,
    key_hash VARCHAR(255) UNIQUE NOT NULL,
    prefix VARCHAR(12) NOT NULL,
    rate_limit_per_min INT DEFAULT 120 NOT NULL,
    is_active BOOLEAN DEFAULT TRUE NOT NULL,
    expires_at TIMESTAMP WITH TIMEZONE,
    created_at TIMESTAMP WITH TIMEZONE DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- 7. Regulatory Audit Trail (OJK & BI Compliance)
CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    actor_id UUID,
    action audit_action NOT NULL,
    resource_name VARCHAR(100) NOT NULL,
    resource_id VARCHAR(255),
    ip_address VARCHAR(45) NOT NULL,
    trace_id VARCHAR(100) NOT NULL,
    payload_snapshot JSONB,
    created_at TIMESTAMP WITH TIMEZONE DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- 8. Indexes
CREATE INDEX IF NOT EXISTS idx_accounts_number ON accounts(account_number);
CREATE INDEX IF NOT EXISTS idx_accounts_customer ON accounts(customer_id);
CREATE INDEX IF NOT EXISTS idx_tx_source_created ON transactions(source_account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tx_dest_created ON transactions(destination_account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tx_idempotency ON transactions(idempotency_key);
CREATE INDEX IF NOT EXISTS idx_ledger_account_created ON ledger_entries(account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_tx ON ledger_entries(transaction_id);
CREATE INDEX IF NOT EXISTS idx_va_number ON virtual_accounts(va_number);
CREATE INDEX IF NOT EXISTS idx_audit_trace ON audit_logs(trace_id);

-- 9. Updated_At Triggers
CREATE OR REPLACE FUNCTION update_timestamp()
RETURNS TRIGGER AS $$
BEGIN
   NEW.updated_at = NOW();
   RETURN NEW;
END;
$$ language 'plpgsql';

DROP TRIGGER IF EXISTS update_customers_modtime ON customers;
CREATE TRIGGER update_customers_modtime BEFORE UPDATE ON customers FOR EACH ROW EXECUTE PROCEDURE update_timestamp();

DROP TRIGGER IF EXISTS update_accounts_modtime ON accounts;
CREATE TRIGGER update_accounts_modtime BEFORE UPDATE ON accounts FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
