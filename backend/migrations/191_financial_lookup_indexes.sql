-- Keep completed external mall orders and bounded financial windows indexable.
-- mall_purchases remains the internal-credit source; payment_orders is the
-- external-payment source and has no reliable cross-table order key.

CREATE INDEX IF NOT EXISTS payment_orders_completed_mall_event_idx
    ON payment_orders ((COALESCE(completed_at, paid_at, created_at)) DESC, id DESC)
    WHERE status = 'COMPLETED'
      AND (currency_product_id IS NOT NULL OR (order_type = 'subscription' AND plan_id IS NOT NULL));

CREATE INDEX IF NOT EXISTS payment_orders_completed_mall_user_event_idx
    ON payment_orders (user_id, (COALESCE(completed_at, paid_at, created_at)) DESC, id DESC)
    WHERE status = 'COMPLETED'
      AND (currency_product_id IS NOT NULL OR (order_type = 'subscription' AND plan_id IS NOT NULL));

-- Checkout and shelf pages aggregate completed sales by product on every read.
-- Keep those GROUP BY scans on compact, product-keyed partial indexes instead
-- of walking unrelated balance-recharge orders.
CREATE INDEX IF NOT EXISTS payment_orders_completed_currency_product_idx
    ON payment_orders (currency_product_id)
    WHERE status = 'COMPLETED' AND currency_product_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS payment_orders_completed_subscription_plan_idx
    ON payment_orders (plan_id)
    WHERE status = 'COMPLETED' AND order_type = 'subscription' AND plan_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS mall_purchases_completed_product_idx
    ON mall_purchases (product_type, product_id)
    WHERE status = 'completed';
