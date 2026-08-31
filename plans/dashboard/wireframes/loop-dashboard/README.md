# Loop dashboard — MVP design spec

*Design handoff. Lives with the plan; see `plans/throughline-loop-visualization-mvp-plan.md` for the engineering plan this amends.*

## Overview

`throughline ui` opens a loopback-only local dashboard for one human reviewer. Its job is narrow: **a human opens it because an agent is blocked**, decides the gates that are waiting, and closes it. Everything else — status, progress, claims — is written by agents and is read-only here.

The MVP is one screen: a shell (objective switcher + count row + two tabs), a **Needs you** queue with a persistent "Objective at a glance" rail, a read-only **Board**, and **one detail drawer** shared by both tabs.

## About the design files

The HTML files next to this document are **design references** — prototypes of intended look and behavior, not production code to copy. The target implementation is the one in `plans/throughline-loop-visualization-mvp-plan.md`:

- vanilla TypeScript, **no UI framework**, two Web Components (`throughline-loop-board`, `throughline-gate-card`)
- esbuild, assets embedded in the Go binary, generated assets committed
- one view model, transport adapters for browser fetch and MCP-App postMessage

So: **re-author the markup and CSS in that environment.** The prototype uses a React-flavoured template runtime purely as a preview harness — none of it should be ported. What must be reproduced exactly is layout, hierarchy, type, color and interaction; those are documented below so the README stands alone.

## Fidelity

**Mid-fidelity, final structure.** Layout, hierarchy, copy, information order, spacing rhythm and interaction model are decided and should be followed closely. Type and color come from a design system (Modernist, tokens listed below); if the shipped UI adopts a different palette, keep the structure and re-map roles (ink / ground / one accent) rather than redesigning.

Deliberate visual rules: **no rounded corners anywhere** (`--radius-*` is 0), 2px rules between major regions and 1px between rows, flush-left labels including inside buttons, one accent used only for "this needs you" and the primary action.

## Screens / views

### 1. Shell (all tabs)

- **Top bar** — 2px bottom rule, ground `--color-neutral-200`, 10px/16px padding, items in one row with 12px gaps: brand `THROUGHLINE` (Archivo 800, 12px, `letter-spacing .06em`, uppercase); label `objective` (600 9.5px, `.1em`, uppercase, `--color-neutral-600`); the **objective switcher button**; spacer; right-side meta (10px monospace, `--color-neutral-600`): `phase <phase> · actor <actor-id> · <workspace path>`.
- **Objective switcher** — the objective name *is* the control. Transparent button, 1px transparent border that becomes `--color-text` on hover/open with a white fill, 5px/8px padding, caret ▼/▲. Opens a 540px panel, absolutely positioned under the bar, `top:100%`, 2px `--color-text` border, `--shadow-lg`, white. Panel header: `Switch objective` + `<workspace> · N objectives`. Each row: 8px square dot (accent when current, `--color-neutral-500` otherwise), title (600 11.5px), meta line `ID · phase X · N items`, and a right-aligned badge — accent-filled `N need you`, otherwise neutral `clear` / `closed`. Hover tint `--color-accent-100`. Footer note: switching reloads the snapshot; gates on the objective you leave stay open.
  - **No objective picker screen.** Launch auto-resolves: `--objective` if given, else the objective with the most blocking gates, else last used. This replaces the plan's "show a picker first when the workspace has several objectives".
- **Count row** — white, 2px bottom rule, seven equal cells divided by 1px rules, each cell: uppercase micro-label above an Archivo 800 18px number. Order: `needs you` (accent-colored number when > 0), `proposed`, `ready`, `in progress`, `in review`, `accepted`, `dormant actors`.
- **Tabs** — `--color-neutral-200`, 2px bottom rule, 1px rules between tabs, 10px/18px padding, 600 11px uppercase `.04em`. Active tab inverts to `--color-text` on white text. `Needs you` carries an accent count pill (600 10px monospace, 1px/5px padding). A right-aligned hint in monospace 10px changes per tab: `click any gate to read the evidence` / `read-only · agents own status`.

### 2. Needs you (default tab)

Two columns: flexible queue column (14px/16px padding, 11px gaps) + fixed 300px rail on a 2px left rule.

Queue column, top to bottom:

1. **Resume blocks** (only when present) — 2px `--color-text` box on `--color-neutral-100`. Header: `Resume` + note "decisions are recorded — these actors are not listening". One block **per actor, not per gate**: actor id (monospace 600 9.5px), count chip, `last call <age>`, the decided gate ids, then the prompt in an inverted code block (`--color-text` ground, `#f3f2f2` text, 10.5px monospace, `white-space:pre-wrap`), then `Copy prompt` (primary) and `Pasted — dismiss` (ghost). The prompt carries **no decision content** — only workspace, actor, and an instruction to call `get_changes` and continue. Copy writes to the clipboard and the label becomes `✓ Copied`.
2. **Live-actor notes** — for actors that decided gates while their session is live: a single line on a 2px left rule, `N recorded · session live, last call <age> · no prompt needed`. No prompt, nothing to do.
3. **Head gate, expanded** — 2px accent border on white. Header row: accent-filled type chip, gate id, right-aligned `waiting <age>`. Body: title (600 14.5px, `text-wrap:pretty`), a `held by <actor>` line with `last call <age>` and a liveness chip (`session live` neutral / `dormant <age>` accent-tinted), a one-line evidence hint in monospace 10px, then actions: **`Review & decide →` (primary)**, the quick verdict (e.g. `Accept revision 3`), and the negative verdict (opens the drawer — negatives always require a rationale, so they never resolve from the queue).
4. **Remaining gates** — full-width rows, 1px `--color-neutral-400` border, white, 10px/12px padding, single line: type chip, id, title (600 12px, flex, truncating), actor, age. Whole row is the button; hover moves the border to accent.
5. **Empty state** — replaces all of the above when the queue drains: `Nothing blocking, nobody waiting` label over an Archivo 800 16px line, plus `Look at the board`. If the queue is empty **at launch**, the Board tab is preselected instead.

**Objective at a glance** rail (`--color-neutral-100`, sections divided by 1px rules, 11px/12px padding, 7px gaps):

- header `Objective at a glance` + `cursor <n>`
- **Phase** — value (600 12px) + one-line note
- **Actors** — one row per registered actor: id (monospace) + state chip; `live <age>` neutral, `dormant <age>` accent-tinted. Liveness = time since that actor's last tool call.
- **Outputs** — key/value grid (104px label column, 10.5px monospace): accepted, in review, profiles
- **Recent decisions** — up to three `age · text` lines

### 3. Board

- A banner strip above the columns (`--color-neutral-200`, 2px bottom rule): `Board` + "Status is agent-owned. Cards cannot be dragged — click a card to read it; to change its course, decide a gate."
- Five equal columns (`Backlog`, `Ready`, `In progress`, `Review`, `Done`) divided by 1px rules; header per column: uppercase label + count in 10px monospace, 2px bottom rule; body 10px padding, 10px gaps.
- **Card** (white, 1px `--color-neutral-400`, 9px/10px, 6px gaps; hover border accent; whole card is a button): id (monospace 600 9.5px) + kind chip + priority chip (`urgent` = accent-tinted, absent = dashed outline chip) / title (600 11.5px) / meta line (`agent:x · live` or `unclaimed` or `accepted 2d ago`) / blocker line when blocked (10px monospace, accent, 2px accent left rule, e.g. `gated · APR-118 needs you`) / progress when criteria exist (`crit 3/5` + a 3px bar filled `--color-neutral-800`). Done-column cards render at 62% opacity.
- Below 900px the five columns collapse to a single selectable status view (per plan) — not drawn.

### 4. Detail drawer (one surface, both tabs)

560px, right-anchored, full height, white, 2px left rule, `--shadow-lg`, over a 45% ink scrim; clicking the scrim closes. Sections in fixed order:

- **Header** (`--color-neutral-200`, 2px bottom rule): type chip + id + a 26×26 square `✕`; title (600 15px); sub-line `actor · waiting <age> · target <ID>`.
- **What is being asked** — gate targets only. Accent-tinted panel (`--color-accent-200`), label + 12px prose in `--color-accent-800`. This is the drawer's reason for existing: it names the decision in one paragraph before showing anything else.
- **Evidence** — label + meta line + a bordered monospace block on `--color-neutral-100`. For a plan/spec revision this is a **diff against the last accepted revision**: added lines tinted accent and colored `--color-accent-800`, removed lines struck through in `--color-neutral-600`, context lines plain (this answers "what was revision 3?" without leaving the dashboard). For an external action it is the exact command, host, network, writes and revert. For a question it is the established options and observations. For an output it is the validation criteria with verdicts. For a plain work item it is the acceptance criteria with passed ones marked.
- **Facts** — 8-pair key/value grid: target, version, requester + age, attention state, profile, objective, supersedes, blocks (varies by gate kind; always includes the version the decision will be written against). **Claim state lives here and only here** — `claim  agent:builder · ttl 11m` or `claim  expired 26m ago`. Board cards never show a TTL; a card whose claim expired reads as unclaimed.
- **Dependencies** — `relation · target · state` rows (`blocks`, `depends on`, `produced by`, `required by`).
- **Activity** — `age · event · actor` rows, newest first, four is enough.
- **Footer** (`--color-neutral-100`, 2px top rule):
  - **Decidable target** — primary verdict, negative verdict, `Not now`, plus a footnote stating what the write does. Choosing the negative verdict (or opening a question gate) reveals a rationale textarea above the buttons; the primary becomes `Confirm <negative>` and stays disabled until the text is non-empty. Rejection, waiver, revocation and objective pause always require a rationale.
  - **On rejection the item does not move.** The gate closes, the rationale is written to the target, and the item keeps its column with a `revision requested · <rationale first line>` blocker line until its actor re-proposes. The human never relocates a card — status stays agent-owned, and a rejected item sitting visibly in Review is the honest picture of where the work is.
  - **Read-only target** — a `read-only` chip and the line "No open gate on this item. Status and progress are written by its actor." A board card whose item *does* have an open gate opens the gate's drawer instead, decision buttons included.

## Interactions & behavior

- **Landing** — queue-first when any gate is open, board otherwise.
- **One detail surface** — gate row, head-gate `Review & decide`, and board card all open the same drawer. Open on cached row data, then refresh from `get_item` / the gate snapshot.
- **Deciding writes state and nothing else.** No message is sent to any agent. Approve/Reject POSTs against `expected_version`; on success the gate leaves the queue, counts decrement, and the next gate becomes the head.
- **Resume is a separate act.** After a decision, if the actor's last tool call is older than **60s**, a resume block appears for that actor. (60s, not 20s: a live session can go quiet through one long tool run, and a resume prompt shown unnecessarily costs the human a pointless paste.) Live actors get a note only. Resume blocks are dismissed manually in the MVP; they should vanish on their own when that actor's next tool call lands.
- **Polling** — activity cursor every 2s, reload the snapshot only when the cursor moves, back off failures to 15s with a manual retry. **Never reorder or replace the gate the human currently has open in the drawer**; queue reordering waits until the drawer closes.
- **409** — `version_conflict` / `approval_stale` refuse the decision. Keep the drawer open, reload it, state what changed and who changed it, and require a fresh decision. Nothing is overwritten.
- **Idempotency** — generate a stable key per user intent in the browser and reuse it across retries.
- **Keyboard/a11y** — every row is a real `<button>`; drawer is a semantic dialog with focus trap and Escape-to-close; focus ring is `2px solid var(--color-accent)` with `2px` offset everywhere; decision results announced via a live region.

## State

| State | Values | Trigger |
| --- | --- | --- |
| `tab` | `queue` \| `board` | tab click; derived at launch from gate count |
| `objective` | objective id | switcher pick; `--objective`; auto-resolve |
| `switcherOpen` | bool | objective button |
| `selection` | `{kind: gate\|item, id}` \| null | row/card click, scrim/✕/Escape |
| `rationale` | string | textarea; gates primary button when required |
| `rejecting` | bool | negative verdict clicked (reveals rationale) |
| `decided` | set of gate ids | successful POST (server truth; local set only bridges the poll) |
| `resumeDismissed` | set of actor ids | dismiss click; should also clear on that actor's next tool call |
| `cursor` | change cursor | every `get_changes` |

## Serving and API

**The dashboard is served by the MCP backend and talks to it over HTTP.** The daemon serves the embedded UI assets and the JSON API from the same loopback origin; the browser never speaks MCP and never holds a credential. The UI is a pure view over one server-computed view model — it derives no status, no readiness and no blocker reason of its own.

- `GET  /api/v1/objectives` — switcher contents.
- `GET  /api/v1/loop?objective_id=` — the whole screen in one payload (see shape below).
- `GET  /api/v1/changes?objective_id=&since=` — cursor delta; `{cursor, changed: bool}` is enough to decide whether to refetch `/loop`.
- `GET  /api/v1/gate/{id}` — the drawer's gate detail (ask text, evidence block, facts, deps, activity).
- `GET  /api/v1/item/{id}` — the drawer's read-only item detail.
- POSTs, one per decision kind, per the plan: plan review, output-profile review, generic approval resolution, action-approval resolution, question resolution, work-item attention resolution, objective pause.

Actor comes from the server session only — never from a request body. Same-origin JSON, `Origin`/`Host` validation, restrictive CSP, SameSite cookies, loopback listen only, dynamic port printed on start.

### `GET /loop` view model

The payload maps one-to-one onto the regions of this spec, so the UI has nothing to compute:

```
objective   { id, title, phase, workspace_path }
counts      { needs_you, proposed, ready, in_progress, in_review, accepted, dormant_actors }
gates       [ { id, kind, title, target_id, target_kind, requester, requested_at,
                waiting_label, expected_version, allowed_decisions[],
                rationale_required_for[], evidence_hint,
                actor: { id, last_call_at, live } } ]        // queue order = server order
columns     [ { key, label, count, cards: [ LoopCard ] } ]
LoopCard    { id, kind, priority, title, meta_label, blocker: {code, label}|null,
              progress: {passed, total}|null, gate_id|null, dimmed }
glance      { phase, phase_note, actors: [ {id, last_call_at, live} ],
              outputs: {accepted, in_review, profiles}, recent_decisions: [ {at, text} ] }
cursor      int
```

Rules the server owns, not the client: queue order, blocker reason codes (computed from readiness facts, never from UI status heuristics), `live` (last tool call within 60s), `allowed_decisions` and `rationale_required_for` per gate kind, and every human-readable label that encodes policy. The client owns only: which tab, which row is selected, textarea contents, and the browser-generated idempotency key.

### Decisions

Every POST carries `{expected_version, idempotency_key, rationale?}` and returns the refreshed gate plus a new cursor. `409` (`version_conflict`, `approval_stale`) refuses the decision — the drawer stays open, reloads, and states what changed. Nothing is overwritten, and no POST notifies any agent: the write is the whole effect, and agents observe it through `get_changes`.

Which MCP tool backs which region: `get_objective_context` → switcher, counts, glance. `list_items` → columns. `get_item` → item drawer. `get_changes(since)` → cursor, liveness, queue. `request_approval` / `request_attention` / `request_action_approval` targets → gates.

## Where this design amends the plan

1. **No objective picker screen** — the top-bar switcher is the only mount; launch auto-resolves (see above).
2. **Board is explicitly read-only and says so** — no drag, no manual status, no claim takeover. The banner states it.
3. **Queue is the landing surface**, not the board, whenever a gate is open.
4. **Resume/doorbell blocks** are new: grouped by actor, prompt carries no decision, manual dismiss for now.
5. **One drawer for gates and cards** rather than a separate gate modal and card drawer.
6. **Inline / MCP-App gate card is postponed.** `show_loop_gate` and `throughline-gate-card` are out of this handoff; explored designs are in the wireframe file (turn 5) for later. Keep the view model transport-agnostic so the inline surface can reuse it.

## Decided (was open)

- **Rejected items stay put**, marked revision-requested with the rationale on the item — see the drawer footer above.
- **Claim TTL appears in the drawer only.** Cards stay quiet; an expired claim reads as unclaimed.
- **Dormant threshold: 60s** since the actor's last tool call.
- **The four remaining gate kinds are not drawn** — they follow the drawer pattern (ask → evidence → facts → footer verdicts) with no new layout. Their evidence blocks:
  - *output-profile activation* — the profile version's structure and semantics, diffed against the version it supersedes; verdicts `Activate` / `Reject` (rationale).
  - *grant revocation* — the live grant: principal, approved-for actor, subject hash, constraints, executions recorded so far; verdicts `Revoke` (rationale) / `Leave active`.
  - *question waiver* — the question, why it was asked, and what proceeds without an answer; verdicts `Waive` (rationale) / `Answer instead` (switches to the answer textarea).
  - *objective pause* — the objective's live actors and claimed items, with the explicit note that pausing changes authoritative state only and does not stop any running process; verdicts `Pause` (rationale) / `Cancel`.

Nothing else is blocking. If a fifth gate kind appears, it needs one paragraph of "what is being asked" and one evidence block — no new screen.

## Design tokens

Colors: ground `#f3f2f2`, ink `#201e1d`, accent `#ec3013`, divider `color-mix(in srgb, #201e1d 40%, transparent)`.
Neutral ramp 100→900: `#f8f4f4` `#eae7e7` `#d7d3d3` `#bab6b6` `#9b9797` `#7d7979` `#605d5d` `#444141` `#2d2b2b`.
Accent ramp 100→900: `#fff2ef` `#ffe0d9` `#ffc4b8` `#ff9783` `#ff563c` `#dd2b0f` `#ae1800` `#7c1405` `#4d170e`.
Card/panel fill `#ffffff`; scrim `color-mix(in srgb, #201e1d 45%, transparent)`.

Type: **Archivo** for both heading (800) and body (400/600). Interface sizes used: 18px/800 (stat numbers), 15px/600 (drawer title), 14.5px/600 (head gate title), 12px/600 (card and row titles at 11.5px), 12px/400 body, 11px/600 uppercase `.04em` (tabs), 10px/600 uppercase `.05em` (buttons), 9.5px/600 uppercase `.1em` (micro labels), 10–10.5px monospace (ids, meta, diffs, key/value). Monospace stack: `ui-monospace, Menlo, monospace`.

Spacing: 4 / 8 / 12 / 16 / 24 / 32. Radius: **0 everywhere**. Shadows: `--shadow-lg: 0 12px 32px color-mix(in srgb,#2d2b2b 22%,transparent)` for the drawer and the switcher panel; nothing else is elevated.

Rules: 2px `--color-text` between major regions (top bar, count row, tabs, column headers, drawer edges); 1px `--color-divider` between rows and cells.

## Assets

None. No images, no icon font — the only glyphs are `▼ ▲ ✕ → ✓`, and Lucide is available if icons are wanted later. Archivo loads from Google Fonts in the prototype; embed it (or substitute a system stack) in the shipped binary since the UI must work offline.

## Files here

- `loop-dashboard-mvp.dc.html` — the implementation candidate. Open it in a browser; it is interactive (switch objectives, decide gates, walk the queue to empty, open board cards).
- `loop-dashboard-explorations.dc.html` — the exploration that led here, newest turn first. Turn 5 holds the postponed inline/MCP-App blocks; turns 1–4 record what was cut and why.
- `support.js`, `_ds/modernist-*/styles.css`, `_ds/modernist-*/_ds_bundle.js` — the preview runtime and the design-system stylesheet the two HTML files load. `styles.css` is the authoritative token source; the shipped UI should copy the tokens out of it rather than depend on it.

These are documentation, not build inputs. Nothing here is compiled or embedded into the binary.
