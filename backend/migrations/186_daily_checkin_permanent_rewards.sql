-- Record permanent-balance rewards alongside the existing temporary-credit
-- snapshot. Existing check-ins remain valid and resolve to a zero permanent
-- reward after upgrade.

ALTER TABLE daily_checkins
    ADD COLUMN IF NOT EXISTS permanent_reward_amount NUMERIC(20,8) NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'daily_checkins_permanent_reward_nonnegative'
          AND conrelid = 'daily_checkins'::regclass
    ) THEN
        ALTER TABLE daily_checkins
            ADD CONSTRAINT daily_checkins_permanent_reward_nonnegative
            CHECK (permanent_reward_amount >= 0);
    END IF;
END
$$;
