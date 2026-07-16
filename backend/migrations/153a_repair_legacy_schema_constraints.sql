-- Repair legacy databases where an older "CREATE TABLE IF NOT EXISTS" shape
-- marked migrations as applied but left primary keys or announcement columns absent.

DO $$
DECLARE
    duplicate_count BIGINT;
BEGIN
    IF to_regclass('public.accounts') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM pg_index i
           WHERE i.indrelid = 'public.accounts'::regclass
             AND i.indisprimary
       ) THEN
        SELECT COUNT(*) INTO duplicate_count
        FROM (
            SELECT id
            FROM public.accounts
            GROUP BY id
            HAVING COUNT(*) > 1
        ) d;

        IF duplicate_count > 0 THEN
            RAISE EXCEPTION 'cannot add accounts primary key: % duplicate id value(s)', duplicate_count;
        END IF;

        ALTER TABLE public.accounts
            ADD CONSTRAINT accounts_pkey PRIMARY KEY (id);
    END IF;
END $$;

DO $$
DECLARE
    duplicate_count BIGINT;
BEGIN
    IF to_regclass('public.ops_error_logs') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM pg_index i
           WHERE i.indrelid = 'public.ops_error_logs'::regclass
             AND i.indisprimary
       ) THEN
        SELECT COUNT(*) INTO duplicate_count
        FROM (
            SELECT id
            FROM public.ops_error_logs
            GROUP BY id
            HAVING COUNT(*) > 1
        ) d;

        IF duplicate_count > 0 THEN
            RAISE EXCEPTION 'cannot add ops_error_logs primary key: % duplicate id value(s)', duplicate_count;
        END IF;

        ALTER TABLE public.ops_error_logs
            ADD CONSTRAINT ops_error_logs_pkey PRIMARY KEY (id);
    END IF;
END $$;

DO $$
DECLARE
    duplicate_count BIGINT;
BEGIN
    IF to_regclass('public.orphan_allowed_groups_audit') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM pg_index i
           WHERE i.indrelid = 'public.orphan_allowed_groups_audit'::regclass
             AND i.indisprimary
       ) THEN
        SELECT COUNT(*) INTO duplicate_count
        FROM (
            SELECT id
            FROM public.orphan_allowed_groups_audit
            GROUP BY id
            HAVING COUNT(*) > 1
        ) d;

        IF duplicate_count > 0 THEN
            RAISE EXCEPTION 'cannot add orphan_allowed_groups_audit primary key: % duplicate id value(s)', duplicate_count;
        END IF;

        ALTER TABLE public.orphan_allowed_groups_audit
            ADD CONSTRAINT orphan_allowed_groups_audit_pkey PRIMARY KEY (id);
    END IF;
END $$;

DO $$
DECLARE
    duplicate_count BIGINT;
BEGIN
    IF to_regclass('public.scheduled_test_plans') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM pg_index i
           WHERE i.indrelid = 'public.scheduled_test_plans'::regclass
             AND i.indisprimary
       ) THEN
        SELECT COUNT(*) INTO duplicate_count
        FROM (
            SELECT id
            FROM public.scheduled_test_plans
            GROUP BY id
            HAVING COUNT(*) > 1
        ) d;

        IF duplicate_count > 0 THEN
            RAISE EXCEPTION 'cannot add scheduled_test_plans primary key: % duplicate id value(s)', duplicate_count;
        END IF;

        ALTER TABLE public.scheduled_test_plans
            ADD CONSTRAINT scheduled_test_plans_pkey PRIMARY KEY (id);
    END IF;
END $$;

DO $$
DECLARE
    duplicate_count BIGINT;
BEGIN
    IF to_regclass('public.user_allowed_groups') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM pg_index i
           WHERE i.indrelid = 'public.user_allowed_groups'::regclass
             AND i.indisprimary
       ) THEN
        SELECT COUNT(*) INTO duplicate_count
        FROM (
            SELECT user_id, group_id
            FROM public.user_allowed_groups
            GROUP BY user_id, group_id
            HAVING COUNT(*) > 1
        ) d;

        IF duplicate_count > 0 THEN
            RAISE EXCEPTION 'cannot add user_allowed_groups primary key: % duplicate pair(s)', duplicate_count;
        END IF;

        ALTER TABLE public.user_allowed_groups
            ADD CONSTRAINT user_allowed_groups_pkey PRIMARY KEY (user_id, group_id);
    END IF;
END $$;

DO $$
DECLARE
    duplicate_count BIGINT;
BEGIN
    IF to_regclass('public.user_group_rate_multipliers') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM pg_index i
           WHERE i.indrelid = 'public.user_group_rate_multipliers'::regclass
             AND i.indisprimary
       ) THEN
        SELECT COUNT(*) INTO duplicate_count
        FROM (
            SELECT user_id, group_id
            FROM public.user_group_rate_multipliers
            GROUP BY user_id, group_id
            HAVING COUNT(*) > 1
        ) d;

        IF duplicate_count > 0 THEN
            RAISE EXCEPTION 'cannot add user_group_rate_multipliers primary key: % duplicate pair(s)', duplicate_count;
        END IF;

        ALTER TABLE public.user_group_rate_multipliers
            ADD CONSTRAINT user_group_rate_multipliers_pkey PRIMARY KEY (user_id, group_id);
    END IF;
END $$;

-- Align serial sequences after primary-key repair.
SELECT setval(
    'public.accounts_id_seq',
    GREATEST(COALESCE((SELECT MAX(id) FROM public.accounts), 0), 1),
    (SELECT MAX(id) IS NOT NULL FROM public.accounts)
)
WHERE to_regclass('public.accounts_id_seq') IS NOT NULL
  AND to_regclass('public.accounts') IS NOT NULL;

SELECT setval(
    'public.ops_error_logs_id_seq',
    GREATEST(COALESCE((SELECT MAX(id) FROM public.ops_error_logs), 0), 1),
    (SELECT MAX(id) IS NOT NULL FROM public.ops_error_logs)
)
WHERE to_regclass('public.ops_error_logs_id_seq') IS NOT NULL
  AND to_regclass('public.ops_error_logs') IS NOT NULL;

SELECT setval(
    'public.orphan_allowed_groups_audit_id_seq',
    GREATEST(COALESCE((SELECT MAX(id) FROM public.orphan_allowed_groups_audit), 0), 1),
    (SELECT MAX(id) IS NOT NULL FROM public.orphan_allowed_groups_audit)
)
WHERE to_regclass('public.orphan_allowed_groups_audit_id_seq') IS NOT NULL
  AND to_regclass('public.orphan_allowed_groups_audit') IS NOT NULL;

SELECT setval(
    'public.scheduled_test_plans_id_seq',
    GREATEST(COALESCE((SELECT MAX(id) FROM public.scheduled_test_plans), 0), 1),
    (SELECT MAX(id) IS NOT NULL FROM public.scheduled_test_plans)
)
WHERE to_regclass('public.scheduled_test_plans_id_seq') IS NOT NULL
  AND to_regclass('public.scheduled_test_plans') IS NOT NULL;

-- Repair legacy announcements table created by an older migration shape.
ALTER TABLE public.announcements
    ADD COLUMN IF NOT EXISTS status VARCHAR(20),
    ADD COLUMN IF NOT EXISTS targeting JSONB,
    ADD COLUMN IF NOT EXISTS starts_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS ends_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS created_by BIGINT,
    ADD COLUMN IF NOT EXISTS updated_by BIGINT,
    ADD COLUMN IF NOT EXISTS notify_mode VARCHAR(20);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'announcements'
          AND column_name = 'is_active'
    ) THEN
        UPDATE public.announcements
        SET status = CASE WHEN COALESCE(is_active, TRUE) THEN 'active' ELSE 'archived' END
        WHERE status IS NULL;
    ELSE
        UPDATE public.announcements
        SET status = 'draft'
        WHERE status IS NULL;
    END IF;
END $$;

UPDATE public.announcements
SET targeting = '{}'::jsonb
WHERE targeting IS NULL;

UPDATE public.announcements
SET notify_mode = 'silent'
WHERE notify_mode IS NULL;

UPDATE public.announcements
SET created_at = NOW()
WHERE created_at IS NULL;

UPDATE public.announcements
SET updated_at = NOW()
WHERE updated_at IS NULL;

ALTER TABLE public.announcements
    ALTER COLUMN status SET DEFAULT 'draft',
    ALTER COLUMN status SET NOT NULL,
    ALTER COLUMN targeting SET DEFAULT '{}'::jsonb,
    ALTER COLUMN targeting SET NOT NULL,
    ALTER COLUMN notify_mode SET DEFAULT 'silent',
    ALTER COLUMN notify_mode SET NOT NULL,
    ALTER COLUMN created_at SET DEFAULT NOW(),
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT NOW(),
    ALTER COLUMN updated_at SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.announcements'::regclass
          AND conname = 'announcements_created_by_fkey'
    ) THEN
        ALTER TABLE public.announcements
            ADD CONSTRAINT announcements_created_by_fkey
            FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.announcements'::regclass
          AND conname = 'announcements_updated_by_fkey'
    ) THEN
        ALTER TABLE public.announcements
            ADD CONSTRAINT announcements_updated_by_fkey
            FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_announcements_status ON public.announcements(status);
CREATE INDEX IF NOT EXISTS idx_announcements_starts_at ON public.announcements(starts_at);
CREATE INDEX IF NOT EXISTS idx_announcements_ends_at ON public.announcements(ends_at);
CREATE INDEX IF NOT EXISTS idx_announcements_created_at ON public.announcements(created_at);

COMMENT ON COLUMN public.announcements.status IS '状态: draft, active, archived';
COMMENT ON COLUMN public.announcements.targeting IS '展示条件（JSON 规则）';
COMMENT ON COLUMN public.announcements.starts_at IS '开始展示时间（为空表示立即生效）';
COMMENT ON COLUMN public.announcements.ends_at IS '结束展示时间（为空表示永久生效）';
