-- Persistent account preferences and a bank-specific hold within the existing
-- aggregate users.frozen_balance. users.balance remains spendable permanent.
CREATE TABLE IF NOT EXISTS bank_auto_exchange_preferences (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    threshold NUMERIC(20,8) NOT NULL DEFAULT 1,
    exchange_amount NUMERIC(20,8) NOT NULL DEFAULT 1,
    frozen_deposit NUMERIC(20,8) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT bank_auto_exchange_preferences_threshold_check
        CHECK (threshold >= 0 AND threshold <= 5),
    CONSTRAINT bank_auto_exchange_preferences_amount_check
        CHECK (exchange_amount >= 0.5 AND exchange_amount <= 5),
    CONSTRAINT bank_auto_exchange_preferences_deposit_check
        CHECK ((enabled AND frozen_deposit = 2 * exchange_amount)
            OR (NOT enabled AND frozen_deposit = 0))
);

-- Widen only the known operation allowlist; preserve every other ledger check
-- and all existing operations, including expiry refunds and super check-in.
ALTER TABLE bank_ledger DROP CONSTRAINT IF EXISTS bank_ledger_operation_check;
ALTER TABLE bank_ledger
    ADD CONSTRAINT bank_ledger_operation_check
    CHECK (operation IN (
        'advance',
        'exchange',
        'debt_offset',
        'permanent_settlement',
        'unused_advance_repayment',
        'early_repay_temporary',
        'early_repay_permanent',
        'exchange_expiry_refund',
        'checkin_super_cost',
        'auto_exchange_deposit_freeze',
        'auto_exchange_deposit_release'
    )) NOT VALID;

-- Deposit movements are neither expenses nor income. Their signed spendable
-- and frozen deltas live in metadata; total permanent assets do not change.
ALTER TABLE bank_ledger
    ADD CONSTRAINT bank_ledger_auto_exchange_deposit_check
    CHECK (operation NOT IN ('auto_exchange_deposit_freeze', 'auto_exchange_deposit_release')
        OR (permanent_delta = 0 AND temporary_delta = 0 AND debt_delta = 0 AND debt_before = debt_after));

-- Retain the existing projection for every old operation. For deposit-only
-- movement rows, derive spendable snapshots from the movement metadata rather
-- than from the zero asset delta. No historical rows are rewritten.
CREATE OR REPLACE FUNCTION capture_bank_ledger_balance_snapshot()
RETURNS TRIGGER AS $$
DECLARE
    current_permanent NUMERIC(20,8);
    current_temporary NUMERIC(20,8);
    spendable_delta NUMERIC(20,8);
BEGIN
    SELECT
        balance,
        COALESCE((
            SELECT SUM(remaining_amount)
            FROM temporary_credit_grants
            WHERE user_id = NEW.user_id
              AND remaining_amount > 0
              AND available_at <= clock_timestamp()
              AND expires_at > clock_timestamp()
        ), 0)
    INTO current_permanent, current_temporary
    FROM users
    WHERE id = NEW.user_id;

    spendable_delta := NEW.permanent_delta;
    IF NEW.operation IN ('auto_exchange_deposit_freeze', 'auto_exchange_deposit_release') THEN
        spendable_delta := (NEW.metadata ->> 'spendable_permanent_delta')::NUMERIC(20,8);
        IF spendable_delta IS NULL THEN
            RAISE EXCEPTION 'bank auto exchange deposit ledger requires spendable_permanent_delta';
        END IF;
    END IF;

    NEW.permanent_balance_after := COALESCE(NEW.permanent_balance_after, current_permanent);
    NEW.temporary_balance_after := COALESCE(NEW.temporary_balance_after, current_temporary);
    NEW.permanent_balance_before := COALESCE(
        NEW.permanent_balance_before,
        NEW.permanent_balance_after - spendable_delta
    );
    NEW.temporary_balance_before := COALESCE(
        NEW.temporary_balance_before,
        NEW.temporary_balance_after - NEW.temporary_delta
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON TABLE bank_auto_exchange_preferences IS
    'Auto exchange preferences; frozen_deposit belongs to bank auto exchange and is excluded from spendable users.balance';
