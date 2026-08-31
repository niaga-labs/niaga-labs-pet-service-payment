# service-payment

Escrow payment service with saga pattern orchestration and compensating transactions.

## Description

This service manages the escrow payment lifecycle for pet transport bookings. It implements the saga pattern to coordinate distributed transactions, holds funds in escrow until delivery confirmation, and handles refunds for cancelled bookings. Integrates with Stripe via an anti-corruption layer.

## Features

- Escrow payment initiation and processing
- Saga orchestration for distributed transactions
- Compensating transactions for failure scenarios
- Platform fee calculation and distribution
- Mock Stripe integration via ACL
- Automatic refund processing
- Payment status tracking and auditing

## API Endpoints

| Method | Endpoint                           | Access | Description                    |
|--------|------------------------------------|--------|--------------------------------|
| POST   | /api/v1/payments/initiate          | Owner  | Initiate escrow payment        |
| GET    | /api/v1/payments/:id               | Auth   | Get payment details            |
| GET    | /api/v1/payments/booking/:bookingId| Auth   | Get payment by booking         |
| POST   | /api/v1/payments/:id/refund        | Admin  | Manual refund processing       |

## Payment Lifecycle

States: `pending` → `held` → `released` / `refunded`

- **pending**: Payment initiated, awaiting confirmation
- **held**: Funds held in escrow
- **released**: Funds distributed to runner and platform
- **refunded**: Funds returned to owner

## Kafka Integration

**Events Published:**
- payment.escrow_held
- payment.escrow_released
- payment.escrow_refunded
- payment.escrow_failed

**Events Consumed:**
- booking.delivery_confirmed (triggers release)
- booking.cancelled (triggers refund)

## Configuration

The service requires the following environment variables:

```
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=password
DB_NAME=payment_db
SERVICE_PORT=8002
KAFKA_BROKERS=localhost:9092
KAFKA_TOPIC_PREFIX=kilat-pet-runner
STRIPE_API_KEY=sk_test_xxx
PLATFORM_FEE_PERCENT=15
```

## Tech Stack

- **Language**: Go 1.24
- **Web Framework**: Gin
- **ORM**: GORM
- **Database**: PostgreSQL
- **Message Queue**: Kafka (shopify/sarama)
- **Payment Gateway**: Stripe (mock ACL implementation)

## Running the Service

```bash
# Install dependencies
go mod download

# Point at the shared dev-infra stack (cd ~/Documents/dev-infra; ./dev.ps1 up kilat)
export DB_HOST=localhost DB_PORT=5432 DB_USER=kilat DB_PASSWORD=kilat_secret
export DB_NAME=kilat_payment DB_SSL_MODE=disable

# Apply the SQL migrations -- run from the repository root, the migration
# source is resolved relative to the working directory
go run ./cmd/migrate

# Start the service
go run ./cmd/server
```

### Migrations

`migrations/` is the single source of truth for the schema, in every environment including
development. `cmd/migrate` applies the files and exits; `cmd/server` applies them at startup
too, so a fresh boot is self-sufficient.

There is deliberately no GORM `AutoMigrate` path. Until KPD-60 the server auto-migrated in
development and ran the SQL migrations everywhere else, and `promos`, `promo_usages` and
`subscriptions` had no SQL migration -- so they existed only on a developer laptop. One path
for all environments is what stops that class of drift returning.

The service will start on port 8002.

## Database Schema

- **payments**: Payment records with escrow state
- **transactions**: Ledger for all payment operations
- **platform_fees**: Platform fee calculations and tracking

## Saga Pattern

The service implements compensating transactions for failure scenarios:
- If escrow hold fails, booking is automatically cancelled
- If release fails, payment remains in held state for manual intervention
- Failed refunds are logged for manual processing
