# ADR 0013: Idempotency records are independent of actor registration

- Status: Accepted
- Date: 2026-08-23

## Decision

Idempotency records retain their caller-supplied `actor_id` as part of their composite key, but do
not reference the `actors` table. MCP mutations therefore require a non-empty actor ID and
idempotency key even when the operation does not otherwise require actor registration.

## Consequences

Durable retries are available for trusted-local callers before registration and remain readable if
an actor record is later unavailable. Operations that require an authenticated or registered actor
continue to check that separately in the application layer. The idempotency table is not an actor
directory: its retained guarantees are only exact actor/key/request matching and replay of the
stored response.
