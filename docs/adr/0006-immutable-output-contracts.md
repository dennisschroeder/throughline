# ADR 0006: Immutable exact-version output contracts

- Status: Accepted
- Date: 2026-08-21

## Decision

An ExpectedOutput references one persisted OutputProfile row by immutable ID, name, and exact
version. Only profiles whose persisted lifecycle state is `active` may be assigned. Instance
contracts are JSON objects interpreted conjunctively with the complete profile definition; they
never replace or subtract profile structure, semantics, or validation. No behavior may branch on a
profile name. A proposal uses the next unused version and identifies the currently active earlier
version it will replace; activation supersedes that exact predecessor transactionally.

## Consequences

The eight initial profiles are ordinary governed rows. Changing structure, semantics, or validation
will require a new profile version rather than reinterpreting prior work. Contract weakening is
impossible because the referenced profile remains independently binding. Field-level evaluation,
OutputRevisions, validation, acceptance, and reuse are deliberately deferred.
