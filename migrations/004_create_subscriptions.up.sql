-- 004_create_subscriptions.sql
-- SubscriptionModel was only ever created by the development AutoMigrate branch
-- in cmd/server/main.go, so "subscriptions" did not exist in any environment
-- that runs the SQL migrations instead. See KPD-60.
--
-- The CHECK constraints mirror the domain: PlanType is basic or premium, and
-- SubStatus is active, cancelled or expired.

CREATE TABLE IF NOT EXISTS subscriptions (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID        NOT NULL,
    plan        VARCHAR(20) NOT NULL CHECK (plan IN ('basic', 'premium')),
    price_cents BIGINT      NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'cancelled', 'expired')),
    auto_renew  BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT subscriptions_validity_window CHECK (expires_at >= started_at)
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_user_active ON subscriptions(user_id) WHERE status = 'active';
