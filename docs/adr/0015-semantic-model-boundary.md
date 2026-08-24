# ADR 0015: Embedded Semantic Model Boundary

## Status

Accepted

## Decision

Throughline maintains an English canonical ontology at `ontology/throughline.json`. A deterministic
Go generator normalizes it into `internal/semanticmodel/model.generated.json`, including stable
content and allowlisted source digests. The binary embeds that generated artifact and exposes its
manifest or bounded sections through the read-only `get_semantic_model` MCP tool.

Graphify remains a development-time discovery and drift-analysis aid. Its output is not a runtime
dependency, release artifact, or authority for semantic changes. MCP initialization receives only a
compact bootstrap with a digest; clients retrieve details lazily through the tool.

## Consequences

The semantic model is available without a database migration or workspace state, and clients can
cache it by `content_digest`. Generation and CI detect stale artifacts. The ontology is intentionally
descriptive: it does not add orchestration, RDF/OWL, semantic search, or external-effect execution.