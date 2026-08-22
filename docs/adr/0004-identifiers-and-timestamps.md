# ADR 0004: UUIDv7 identifiers and UTC timestamps

- Status: Accepted
- Date: 2026-08-21

## Decision

Generate UUIDv7 internal identifiers in the application layer through an injectable port. Store
timestamps as UTC RFC3339Nano text and inject the application clock. Human-readable objective and
work-item keys remain distinct explicit fields. Built-in seed rows use fixed UUIDv7 values so their
identities are deterministic across workspaces.

## Consequences

IDs are globally unique and approximately time ordered without a database sequence. Tests remain
deterministic through fake clocks and ID generators. Timestamp text is portable and inspectable;
all comparisons must retain the normalized UTC representation.
