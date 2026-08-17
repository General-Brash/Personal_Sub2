CREATE TABLE IF NOT EXISTS payment_refund_attempts (
    id BIGSERIAL PRIMARY KEY,
    attempt_id VARCHAR(64) NOT NULL UNIQUE,
    order_id BIGINT NOT NULL REFERENCES payment_orders(id) ON DELETE CASCADE,
    refund_amount NUMERIC(20,8) NOT NULL,
    gateway_amount NUMERIC(20,8) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    original_order_status VARCHAR(30) NOT NULL,
    source VARCHAR(30) NOT NULL DEFAULT 'admin',
    deduct_balance BOOLEAN NOT NULL,
    force BOOLEAN NOT NULL,
    deduction_type VARCHAR(20) NOT NULL DEFAULT 'none',
    held_balance_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    subscription_id BIGINT NULL REFERENCES user_subscriptions(id) ON DELETE SET NULL,
    subscription_days INTEGER NOT NULL DEFAULT 0,
    subscription_original_expires_at TIMESTAMPTZ NULL,
    subscription_original_status VARCHAR(20) NULL,
    subscription_revoked BOOLEAN NOT NULL DEFAULT false,
    deduction_state VARCHAR(20) NOT NULL DEFAULT 'none',
    provider_refund_id VARCHAR(128) NULL,
    provider_state VARCHAR(20) NOT NULL DEFAULT 'calling',
    provider_result TEXT NOT NULL DEFAULT '',
    manual_review BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT payment_refund_attempts_amount_check CHECK (refund_amount > 0 AND gateway_amount >= 0),
    CONSTRAINT payment_refund_attempts_held_balance_check CHECK (held_balance_amount >= 0),
    CONSTRAINT payment_refund_attempts_subscription_days_check CHECK (subscription_days >= 0),
    CONSTRAINT payment_refund_attempts_source_check CHECK (source IN ('admin', 'purchase_limit_rejected')),
    CONSTRAINT payment_refund_attempts_deduction_type_check CHECK (deduction_type IN ('none', 'balance', 'subscription')),
    CONSTRAINT payment_refund_attempts_deduction_state_check CHECK (deduction_state IN ('none', 'held', 'consumed', 'returned')),
    CONSTRAINT payment_refund_attempts_provider_state_check CHECK (provider_state IN ('calling', 'pending', 'succeeded', 'failed', 'canceled', 'unknown')),
    CONSTRAINT payment_refund_attempts_hold_shape_check CHECK (
        (deduction_state = 'none' AND held_balance_amount = 0 AND subscription_days = 0)
        OR deduction_state IN ('held', 'consumed', 'returned')
    )
);

CREATE INDEX IF NOT EXISTS payment_refund_attempts_order_created_idx
    ON payment_refund_attempts (order_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS payment_refund_attempts_order_state_idx
    ON payment_refund_attempts (order_id, provider_state);
CREATE INDEX IF NOT EXISTS payment_refund_attempts_manual_review_idx
    ON payment_refund_attempts (manual_review, provider_state)
    WHERE manual_review = true;