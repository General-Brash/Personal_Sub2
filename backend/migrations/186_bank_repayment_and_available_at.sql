-- Bank early repayment, unused-advance settlement, and scheduled temporary credits.
-- Ledger amounts keep the existing NUMERIC(20,8) contract.

ALTER TABLE temporary_credit_grants
    ADD COLUMN IF NOT EXISTS available_at TIMESTAMPTZ NULL;

UPDATE temporary_credit_grants
SET available_at = created_at
WHERE available_at IS NULL;

ALTER TABLE temporary_credit_grants
    ALTER COLUMN available_at SET DEFAULT NOW();

ALTER TABLE temporary_credit_grants
    ALTER COLUMN available_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_temporary_credit_grants_available_fefo
    ON temporary_credit_grants (user_id, available_at, expires_at, id)
    WHERE remaining_amount > 0;

ALTER TABLE bank_loans
    ADD COLUMN IF NOT EXISTS unused_credit_settled_at TIMESTAMPTZ NULL;

ALTER TABLE bank_loans
    ADD COLUMN IF NOT EXISTS unused_credit_amount NUMERIC(20,8) NOT NULL DEFAULT 0;

ALTER TABLE bank_loans
    ADD COLUMN IF NOT EXISTS unused_debt_repaid NUMERIC(20,8) NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'bank_loans_unused_credit_amount_nonnegative'
          AND conrelid = 'bank_loans'::regclass
    ) THEN
        ALTER TABLE bank_loans
            ADD CONSTRAINT bank_loans_unused_credit_amount_nonnegative
            CHECK (unused_credit_amount >= 0 AND unused_credit_amount <= principal);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'bank_loans_unused_debt_repaid_nonnegative'
          AND conrelid = 'bank_loans'::regclass
    ) THEN
        ALTER TABLE bank_loans
            ADD CONSTRAINT bank_loans_unused_debt_repaid_nonnegative
            CHECK (unused_debt_repaid >= 0 AND unused_debt_repaid <= principal);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS bank_loans_unused_credit_due_idx
    ON bank_loans (grant_expires_at, id)
    WHERE status IN ('active', 'repaid') AND unused_credit_settled_at IS NULL;

INSERT INTO settings (key, value, updated_at)
VALUES
    ('bank_unused_advance_debt_reduction_ratio', '0.75000000', NOW()),
    ('bank_early_repay_temporary_ratio', '1.00000000', NOW()),
    ('bank_early_repay_permanent_ratio', '2.00000000', NOW())
ON CONFLICT (key) DO NOTHING;
