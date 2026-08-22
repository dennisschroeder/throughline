# ADR 0007: Canonical authorization subjects and delegated authority

- Status: Accepted
- Date: 2026-08-21

## Decision

Capability and authority are independent. A future AuthorityGrant will bind one principal to the
SHA-256 digest of one exact ExternalAction revision's AuthorizationSubject plus constraints and
lifetime. Workgraph records this chain but never executes the effect.

AuthorizationSubject fields are exactly `action_type`, `target`, `arguments`, `scope`,
`permissions`, `credential_requirements`, and `constraints`. Titles, rationale, progress, and UI
metadata are excluded. Unknown or duplicate object keys are rejected.

Workgraph Canonical JSON v1:

1. All seven fields are required; target, scope, and constraints are objects and arguments is an
   ordered array.
2. Object keys use lexicographic order recursively and insignificant whitespace is removed.
3. Permission and credential-requirement arrays are duplicate-free string sets and sort
   lexicographically; argument array order remains significant.
4. JSON strings and numbers use Go `encoding/json` encoding after lossless `json.Number` parsing.
   `<`, `>`, and `&` remain unescaped UTF-8; U+2028 and U+2029 use lowercase `\u2028` and
   `\u2029` escapes.
5. The lowercase hexadecimal SHA-256 digest of the UTF-8 canonical bytes is the subject hash.

The fixture under `internal/domain/authority/testdata` freezes input, canonical bytes, and digest
for future adapters and cross-language implementations.

## Consequences

Object or set ordering cannot create a different grant, while any authorization-relevant value
change does. Payload revisioning, grants, authorization checks, identity authentication, and
external-action persistence remain outside this milestone.
