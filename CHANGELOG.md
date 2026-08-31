# Changelog

All notable changes to service-payment are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `cmd/migrate`: standalone command that applies the golang-migrate files in
  `migrations/` and exits. There was previously no way to apply the SQL schema
  without booting the service outside `APP_ENV=development`. (KPD-4)
- `CHANGELOG.md`: this file. Partially advances KPD-52.

- `migrations/003_create_promos.{up,down}.sql` and
  `migrations/004_create_subscriptions.{up,down}.sql`: SQL schema for `promos`,
  `promo_usages` and `subscriptions`. All three had GORM models but no SQL
  migration, so they existed only under `APP_ENV=development`. (KPD-60)

### Fixed

- `promos`, `promo_usages` and `subscriptions` no longer exist in development
  only. This unblocks KPD-39, which integrates `/promos/active` and
  `/promos/validate` into checkout pricing. (KPD-60)

### Changed

- README: the run block now points at the shared dev-infra stack and documents the
  single migration path.
- `cmd/server`: the development-only GORM `AutoMigrate` branch is gone. Every model
  in this service now has a SQL migration, so the migrations own the schema in all
  environments and development still gets it automatically at startup. (KPD-60)

### Notes

- Migrations applied to `kilat_payment` on the shared dev-infra stack as part of KPD-4.
