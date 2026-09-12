-- Preserve the invitation reservation invariant when a claimed user is deleted.
-- Migration 237 used ON DELETE SET NULL for claimed_user_id, which can leave
-- status='claimed' with a NULL claimed_user_id and violate the claim check.
DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    ALTER TABLE player_invitation_reservations
        DROP CONSTRAINT IF EXISTS player_invitation_reservations_claimed_user_id_fkey;

    SELECT con.conname
      INTO constraint_name
      FROM pg_constraint con
      JOIN pg_class rel ON rel.oid = con.conrelid
      WHERE rel.relname = 'player_invitation_reservations'
        AND con.contype = 'f'
        AND pg_get_constraintdef(con.oid) ILIKE '%FOREIGN KEY (claimed_user_id)%';

    IF constraint_name IS NOT NULL THEN
        EXECUTE format(
            'ALTER TABLE player_invitation_reservations DROP CONSTRAINT %I',
            constraint_name
        );
    END IF;

    ALTER TABLE player_invitation_reservations
        ADD CONSTRAINT player_invitation_reservations_claimed_user_id_fkey
        FOREIGN KEY (claimed_user_id) REFERENCES users(id) ON DELETE CASCADE;
END $$;
