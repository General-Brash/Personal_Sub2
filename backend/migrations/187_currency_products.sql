-- Fixed permanent-balance products and immutable payment-order snapshots.
-- The product relation is intentionally not a foreign key: deleted products
-- must not invalidate historical or pending payment orders.

-- The migration runner wraps this file in a transaction. Widening legacy
-- NUMERIC columns takes a strong table lock, so fail fast when the lock cannot
-- be acquired and bound the rewrite instead of blocking checkout indefinitely.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '15min';

CREATE TABLE IF NOT EXISTS currency_products (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    payment_price NUMERIC(20,2) NOT NULL CHECK (payment_price > 0),
    credited_permanent_amount NUMERIC(20,8) NOT NULL CHECK (credited_permanent_amount > 0),
    sort_order INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    for_sale BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_currency_products_sale_order
    ON currency_products (is_active, for_sale, sort_order, id);

ALTER TABLE payment_orders
    ALTER COLUMN amount TYPE NUMERIC(20,8),
    ALTER COLUMN refund_amount TYPE NUMERIC(20,8);

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS currency_product_id BIGINT,
    ADD COLUMN IF NOT EXISTS currency_product_name VARCHAR(100),
    ADD COLUMN IF NOT EXISTS currency_product_payment_price NUMERIC(20,2),
    ADD COLUMN IF NOT EXISTS currency_product_credited_amount NUMERIC(20,8);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'payment_orders_currency_product_snapshot_check'
    ) THEN
        ALTER TABLE payment_orders
            ADD CONSTRAINT payment_orders_currency_product_snapshot_check CHECK (
                (currency_product_id IS NULL
                 AND currency_product_name IS NULL
                 AND currency_product_payment_price IS NULL
                 AND currency_product_credited_amount IS NULL)
                OR
                (currency_product_id IS NOT NULL
                 AND currency_product_name IS NOT NULL
                 AND currency_product_payment_price IS NOT NULL
                 AND currency_product_payment_price > 0
                 AND currency_product_credited_amount IS NOT NULL
                 AND currency_product_credited_amount > 0)
            );
    END IF;
END $$;
