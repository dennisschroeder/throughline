# Semantic Model and MCP Bootstrap Plan

**Status:** Planned

## Summary

Create a versioned, implementation-independent JSON ontology for Throughline, use Graphify as a
development-time discovery and drift-analysis tool, embed a deterministic generated model in every
binary, and expose it through:

- a compact semantic bootstrap in MCP `discover` and legacy `initialize` instructions;
- one read-only `get_semantic_model` tool for lazy retrieval of details.

The ontology describes Throughline's authoritative work, output, validation, reuse, and authority
semantics. It does not model agent orchestration and does not introduce RDF, OWL, semantic search,
or runtime Graphify dependencies.

## Implementation changes

### Canonical model and generation

- Add an English canonical specification at `ontology/throughline.json` containing:
  - `schema_version` and semantic `model_version`;
  - a compact `bootstrap`;
  - entities and definitions;
  - directed relations and cardinalities;
  - lifecycle states and valid transitions;
  - deterministic invariants;
  - repository-relative source mappings using symbols or headings rather than fragile line
    numbers.
- Use stable `snake_case` identifiers such as `work_item`, `authority_grant`, and
  `work_item_depends_on_work_item`.
- Generate normalized `internal/semanticmodel/model.generated.json` with:
  - deterministic ordering and formatting;
  - `content_digest` using SHA-256 over canonical model content excluding the digest field;
  - `source_digest` over the sorted, allowlisted domain, MCP, migration, and architectural source
    files;
  - no timestamps or machine-specific paths.
- Add a small Go package that embeds the generated JSON, exposes parsed metadata and sections, and
  validates it once. A malformed artifact produces a structured tool error and falls back to the
  existing operational server instructions. CI must prevent such a binary from being released.
- Add `go generate` support and a generator command. CI and release workflows run generation and
  fail when `git diff` shows stale generated output.
- Keep Graphify outside normal CI and release dependencies. It assists ontology maintenance, while
  deterministic generation uses only committed English sources.

### Graphify workflow

- Perform one full directed Graphify rebuild first because the current ignored graph still contains
  pre-rename `workgraph` identifiers.
- For later ontology work:
  1. Run Graphify incrementally in directed mode.
  2. Compare graph entities and relations against the canonical ontology.
  3. Report missing mappings, renamed symbols, unresolved relation endpoints, and candidate
     concepts.
  4. Review semantic changes before updating the canonical JSON.
  5. Run deterministic generation and tests.
- Keep Graphify output ignored and out of release artifacts. Only the reviewed semantic projection
  is authoritative.
- Record the Graphify boundary and eager-bootstrap/lazy-detail decision in ADR `0015`.

### MCP initialization

- Build `serverInstructions` from the existing operational workflow instructions and the generated
  semantic bootstrap.
- Limit the complete initialization instructions to 2 KiB.
- Include:
  - Throughline's role and orchestration boundary;
  - semantic model version and content digest;
  - the work/output and external-action/authority chains;
  - critical invariants, including "Capability does not imply Authority" and "Throughline never
    performs external effects";
  - guidance to call `get_semantic_model` for details.
- Rely on the Go SDK to return the same instructions through current `server/discover` and legacy
  `initialize`.
- Do not place the full model in initialization.
- Do not add an MCP Resource or custom capability extension in this version. Host handling is
  inconsistent, while a model-controlled tool is portable.

## Public MCP interface

Add one read-only tool:

```text
get_semantic_model
```

Input:

```json
{
  "section": "manifest | entities | relations | lifecycles | invariants | source_mappings | full",
  "ids": ["optional", "stable_ids"]
}
```

Behavior:

- `section` defaults to `manifest`.
- `ids` is optional, unique, and limited to 50 values.
- `manifest` returns versions, digests, bootstrap, available sections, and section counts.
- Section views return only the requested records.
- `full` returns the complete embedded model.
- Unknown IDs are returned in `not_found_ids`; valid matches are still returned.
- The response retains Throughline's standard workspace and change-cursor envelope, while
  `content_digest` is the model cache key.
- The tool is annotated `readOnlyHint: true`.
- No SQLite state, activity entry, authorization, actor registration, or idempotency key is involved.

Compatibility rules:

- Existing MCP tools and responses remain unchanged.
- Additive concepts increment the semantic model's minor version.
- Corrections increment its patch version.
- Breaking meaning, identifier, or invariant changes increment its major version.
- Changes to the JSON wire structure increment `schema_version` independently.

## Test plan

- Generator tests verify byte-for-byte determinism, canonical ordering, digest stability, and the
  exclusion of timestamps and absolute paths.
- Model validation tests verify unique IDs, valid relation endpoints, valid lifecycle transitions,
  referenced invariants, existing source mappings, and the 2 KiB bootstrap limit.
- Drift tests recompute `source_digest` and generated JSON; CI fails if either is stale.
- MCP contract tests verify:
  - initialization instructions contain the model version, digest, core invariants, and lazy-loading
    guidance;
  - instructions arrive through current discovery and legacy initialization;
  - `get_semantic_model` appears in `tools/list` as read-only;
  - default manifest, every section, filtering, unknown IDs, and full retrieval conform to JSON
    Schema;
  - the tool performs no database mutation.
- Release verification runs formatting, vet, all tests, normal and `CGO_ENABLED=0` builds, plus a
  GoReleaser snapshot smoke test proving that the released binary contains the model.

## Assumptions and defaults

- All committed specifications, identifiers, descriptions, instructions, tool contracts, tests,
  ADRs, and documentation are written in English.
- The canonical JSON is reviewed product semantics. Raw or inferred Graphify edges never become
  authoritative automatically.
- The model describes Throughline itself, not the contents of an individual workspace.
- The semantic artifact is bundled with the binary and requires no database migration.
- "Updated with every build" means deterministic generation and drift verification in the
  documented CI and release build pipeline. Graphify itself runs during ontology maintenance, not
  inside every build.
