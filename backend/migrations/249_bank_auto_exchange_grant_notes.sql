-- Permit the exact auto-exchange note while preserving all other source rules.
-- NOT VALID skips the historical scan; new inserts and updates remain checked.
ALTER TABLE temporary_credit_grants
    DROP CONSTRAINT IF EXISTS temporary_credit_grants_source_check;

ALTER TABLE temporary_credit_grants
    ADD CONSTRAINT temporary_credit_grants_source_check CHECK (
        (source = 'checkin' AND checkin_id IS NOT NULL AND granted_by IS NULL AND notes = ''
            AND mall_purchase_id IS NULL AND daily_subscription_id IS NULL AND scheduled_date IS NULL)
        OR
        (source = 'admin_grant' AND checkin_id IS NULL AND granted_by IS NOT NULL
            AND mall_purchase_id IS NULL AND daily_subscription_id IS NULL AND scheduled_date IS NULL)
        OR
        (source = 'bank_advance' AND checkin_id IS NULL AND granted_by IS NULL AND notes = ''
            AND mall_purchase_id IS NULL AND daily_subscription_id IS NULL AND scheduled_date IS NULL)
        OR
        (source = 'bank_exchange' AND checkin_id IS NULL AND granted_by IS NULL
            AND notes IN ('', U&'\81EA\52A8\5151\6362')
            AND mall_purchase_id IS NULL AND daily_subscription_id IS NULL AND scheduled_date IS NULL)
        OR
        (source = 'mall_product' AND checkin_id IS NULL AND granted_by IS NULL AND notes = ''
            AND mall_purchase_id IS NOT NULL AND daily_subscription_id IS NULL AND scheduled_date IS NULL)
        OR
        (source = 'subscription' AND checkin_id IS NULL AND granted_by IS NULL AND notes = ''
            AND mall_purchase_id IS NOT NULL AND daily_subscription_id IS NOT NULL AND scheduled_date IS NOT NULL)
    ) NOT VALID;
