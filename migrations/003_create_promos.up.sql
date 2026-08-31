-- 003_create_promos.sql
-- PromoModel and PromoUsageModel were only ever created by the development
-- AutoMigrate branch in cmd/server/main.go, so neither table existed in any
-- environment that runs the SQL migrations instead. See KPD-60.
--
-- This blocks epic KPD-37 (MVP-GAP-07 Promo apply during checkout): KPD-39
-- integrates /promos/active and /promos/validate, which read these tables.
--
-- The CHECK constraints mirror NewPromoCode in internal/domain/promo/promo.go:
-- a known discount type, a positive discount value, percentages capped at 100,
-- and a validity window that does not run backwards.

CREATE TABLE IF NOT EXISTS promos (
    id                 UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    code               VARCHAR(50) NOT NULL UNIQUE,
    discount_type      VARCHAR(20) NOT NULL CHECK (discount_type IN ('percentage', 'fixed')),
    discount_value     BIGINT      NOT NULL CHECK (discount_value > 0),
    min_amount_cents   BIGINT      NOT NULL DEFAULT 0,
    max_discount_cents BIGINT      NOT NULL DEFAULT 0,
    max_uses           INT         NOT NULL DEFAULT 0 CHECK (max_uses >= 0),
    current_uses       INT         NOT NULL DEFAULT 0 CHECK (current_uses >= 0),
    valid_from         TIMESTAMPTZ NOT NULL,
    valid_until        TIMESTAMPTZ NOT NULL,
    created_by         UUID        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT promos_percentage_max_100 CHECK (discount_type <> 'percentage' OR discount_value <= 100),
    CONSTRAINT promos_validity_window     CHECK (valid_until >= valid_from)
);

-- /promos/active lists what is currently redeemable.
CREATE INDEX IF NOT EXISTS idx_promos_validity ON promos(valid_from, valid_until);

CREATE TABLE IF NOT EXISTS promo_usages (
    id             UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    promo_id       UUID        NOT NULL REFERENCES promos(id) ON DELETE CASCADE,
    user_id        UUID        NOT NULL,
    booking_id     UUID        NOT NULL,
    discount_cents BIGINT      NOT NULL,
    used_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_promo_usages_promo_id ON promo_usages(promo_id);
CREATE INDEX IF NOT EXISTS idx_promo_usages_user_id ON promo_usages(user_id);
