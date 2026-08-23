package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

func (r *transactionRepository) CreateActor(ctx context.Context, actor work.Actor) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO actors (id, kind, display_name, created_at) VALUES (?, ?, ?, ?)`,
		actor.ID, actor.Kind, actor.DisplayName, formatTime(actor.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert actor: %w", err)
	}
	return nil
}

func (r *transactionRepository) Actor(ctx context.Context, id string) (work.Actor, error) {
	actor, err := scanActor(r.transaction.QueryRowContext(ctx, `
SELECT id, kind, display_name, created_at FROM actors WHERE id = ?`, id))
	return actor, mapNotFound(err)
}

func (r *transactionRepository) CreateCapability(ctx context.Context, capability work.Capability) error {
	_, err := r.transaction.ExecContext(ctx,
		"INSERT OR IGNORE INTO capabilities (slug, description) VALUES (?, ?)", capability.Slug, capability.Description)
	if err != nil {
		return fmt.Errorf("insert capability: %w", err)
	}
	return nil
}

func (r *transactionRepository) AssignActorCapability(ctx context.Context, actorID, capability string) error {
	_, err := r.transaction.ExecContext(ctx,
		"INSERT OR IGNORE INTO actor_capabilities (actor_id, capability_slug) VALUES (?, ?)", actorID, capability)
	if err != nil {
		return fmt.Errorf("assign actor capability: %w", err)
	}
	return nil
}

func (r *transactionRepository) ActorHasCapabilities(ctx context.Context, actorID string, capabilities []string) (bool, error) {
	if len(capabilities) == 0 {
		return true, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(capabilities)), ",")
	arguments := make([]any, 0, len(capabilities)+1)
	arguments = append(arguments, actorID)
	for _, capability := range capabilities {
		arguments = append(arguments, capability)
	}
	var count int
	if err := r.transaction.QueryRowContext(ctx, `
SELECT COUNT(*) FROM actor_capabilities
WHERE actor_id = ? AND capability_slug IN (`+placeholders+`)`, arguments...).Scan(&count); err != nil {
		return false, fmt.Errorf("query actor capabilities: %w", err)
	}
	return count == len(capabilities), nil
}

func (r *transactionRepository) RequiredCapabilities(ctx context.Context, workItemID string) ([]string, error) {
	rows, err := r.transaction.QueryContext(ctx,
		"SELECT capability_slug FROM work_item_capabilities WHERE work_item_id = ? ORDER BY capability_slug", workItemID)
	if err != nil {
		return nil, fmt.Errorf("query required capabilities: %w", err)
	}
	defer rows.Close()
	var capabilities []string
	for rows.Next() {
		var capability string
		if err := rows.Scan(&capability); err != nil {
			return nil, err
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities, rows.Err()
}

func (r *transactionRepository) PlanApprovedForWorkItem(ctx context.Context, workItemID string) (bool, error) {
	return queryBoolean(ctx, r.transaction, `
SELECT EXISTS(
  SELECT 1 FROM work_items item
  JOIN plans plan ON plan.id = item.plan_id
  WHERE item.id = ? AND plan.commitment_state = 'approved'
)`, workItemID)
}

func (r *transactionRepository) HasOpenBlocker(ctx context.Context, workItemID string) (bool, error) {
	return queryBoolean(ctx, r.transaction, `SELECT EXISTS(SELECT 1 FROM questions WHERE work_item_id = ? AND status = 'open') OR EXISTS(SELECT 1 FROM manual_blockers WHERE work_item_id = ? AND status = 'active')`, workItemID, workItemID)
}

func (r *transactionRepository) CreateManualBlocker(ctx context.Context, blocker work.ManualBlocker) error {
	_, err := r.transaction.ExecContext(ctx, `INSERT INTO manual_blockers (id, work_item_id, reason, status, created_by, created_at, resolved_by, resolved_at, resolution) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, blocker.ID, blocker.WorkItemID, blocker.Reason, blocker.Status, blocker.CreatedBy, formatTime(blocker.CreatedAt), nullableString(blocker.ResolvedBy), nullableTime(blocker.ResolvedAt), blocker.Resolution)
	return err
}

func (r *transactionRepository) ManualBlocker(ctx context.Context, id string) (work.ManualBlocker, error) {
	var blocker work.ManualBlocker
	var createdAt, resolvedAt string
	err := r.transaction.QueryRowContext(ctx, `SELECT id, work_item_id, reason, status, created_by, created_at, COALESCE(resolved_by, ''), COALESCE(resolved_at, ''), resolution FROM manual_blockers WHERE id = ?`, id).Scan(&blocker.ID, &blocker.WorkItemID, &blocker.Reason, &blocker.Status, &blocker.CreatedBy, &createdAt, &blocker.ResolvedBy, &resolvedAt, &blocker.Resolution)
	if err != nil {
		return work.ManualBlocker{}, mapNotFound(err)
	}
	blocker.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return work.ManualBlocker{}, err
	}
	if resolvedAt != "" {
		blocker.ResolvedAt, err = parseTime(resolvedAt)
		if err != nil {
			return work.ManualBlocker{}, err
		}
	}
	return blocker, nil
}

func (r *transactionRepository) UpdateManualBlocker(ctx context.Context, blocker work.ManualBlocker) error {
	result, err := r.transaction.ExecContext(ctx, `UPDATE manual_blockers SET status = ?, resolved_by = ?, resolved_at = ?, resolution = ? WHERE id = ? AND status = 'active'`, blocker.Status, blocker.ResolvedBy, formatTime(blocker.ResolvedAt), blocker.Resolution, blocker.ID)
	if err != nil {
		return err
	}
	return requireChanged(result)
}

func (r *transactionRepository) WorkItemApprovalSatisfied(ctx context.Context, workItemID, actorID string, now time.Time) (bool, error) {
	return queryBoolean(ctx, r.transaction, `
SELECT EXISTS(
  SELECT 1 FROM approvals
  WHERE work_item_id = ? AND approved_for_actor_id = ? AND status = 'approved'
    AND (expires_at IS NULL OR expires_at > ?)
)`, workItemID, actorID, formatTime(now))
}

func (r *transactionRepository) CreateWorkItemExecutionApproval(ctx context.Context, approval work.ExecutionApproval) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO approvals
  (id, work_item_id, approved_for_actor_id, expires_at, request, status, requested_by, requested_at, resolved_by, resolved_at, rationale)
VALUES (?, ?, ?, ?, ?, 'approved', ?, ?, ?, ?, ?)`, approval.ID, approval.WorkItemID, approval.ApprovedForActorID,
		nullableTimePtr(approval.ExpiresAt), approval.Request, approval.RequestedBy, formatTime(approval.RequestedAt),
		approval.ResolvedBy, formatTime(approval.ResolvedAt), approval.Rationale)
	if err != nil {
		return fmt.Errorf("insert work item execution approval: %w", err)
	}
	return nil
}

func (r *transactionRepository) ActiveClaim(ctx context.Context, workItemID string, now time.Time) (*work.Claim, error) {
	claim, err := scanClaim(r.transaction.QueryRowContext(ctx, `
SELECT id, work_item_id, actor_id, acquired_at, expires_at, released_at, release_reason
FROM claims
WHERE work_item_id = ? AND released_at IS NULL AND expires_at > ?
ORDER BY acquired_at DESC LIMIT 1`, workItemID, formatTime(now)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query active claim: %w", err)
	}
	return &claim, nil
}

func (r *transactionRepository) ExpireClaims(ctx context.Context, workItemID string, now time.Time) ([]work.Claim, error) {
	rows, err := r.transaction.QueryContext(ctx, `
SELECT id, work_item_id, actor_id, acquired_at, expires_at, released_at, release_reason
FROM claims WHERE work_item_id = ? AND released_at IS NULL AND expires_at <= ?`, workItemID, formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("query expired claims: %w", err)
	}
	defer rows.Close()
	var expired []work.Claim
	for rows.Next() {
		claim, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		claim.ReleasedAt = now.UTC()
		claim.ReleaseReason = "lease_expired"
		result, err := r.transaction.ExecContext(ctx, `
UPDATE claims SET released_at = ?, release_reason = ? WHERE id = ? AND released_at IS NULL`,
			formatTime(claim.ReleasedAt), claim.ReleaseReason, claim.ID)
		if err != nil {
			return nil, fmt.Errorf("expire claim: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("read expired claim rows: %w", err)
		}
		if changed == 1 {
			expired = append(expired, claim)
		}
	}
	return expired, rows.Err()
}

func (r *transactionRepository) CreateClaim(ctx context.Context, claim work.Claim) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO claims (id, work_item_id, actor_id, acquired_at, expires_at, released_at, release_reason)
VALUES (?, ?, ?, ?, ?, ?, ?)`, claim.ID, claim.WorkItemID, claim.ActorID, formatTime(claim.AcquiredAt),
		formatTime(claim.ExpiresAt), nullableTime(claim.ReleasedAt), claim.ReleaseReason)
	if err != nil {
		if strings.Contains(err.Error(), "one_unreleased_claim_per_item") || strings.Contains(err.Error(), "UNIQUE constraint failed: claims.work_item_id") {
			return ports.ErrClaimConflict
		}
		return fmt.Errorf("insert claim: %w", err)
	}
	return nil
}

func (r *transactionRepository) Claim(ctx context.Context, id string) (work.Claim, error) {
	claim, err := scanClaim(r.transaction.QueryRowContext(ctx, `
SELECT id, work_item_id, actor_id, acquired_at, expires_at, released_at, release_reason
FROM claims WHERE id = ?`, id))
	return claim, mapNotFound(err)
}

func (r *transactionRepository) RenewClaim(ctx context.Context, claim work.Claim, now time.Time) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE claims SET expires_at = ?
WHERE id = ? AND actor_id = ? AND released_at IS NULL AND expires_at > ?`,
		formatTime(claim.ExpiresAt), claim.ID, claim.ActorID, formatTime(now))
	if err != nil {
		return fmt.Errorf("renew claim: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read renewed claim rows: %w", err)
	}
	if changed != 1 {
		return ports.ErrClaimConflict
	}
	return nil
}

func (r *transactionRepository) ReleaseClaim(ctx context.Context, claim work.Claim) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE claims SET released_at = ?, release_reason = ?
WHERE id = ? AND actor_id = ? AND released_at IS NULL`,
		formatTime(claim.ReleasedAt), claim.ReleaseReason, claim.ID, claim.ActorID)
	if err != nil {
		return fmt.Errorf("release claim: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read released claim rows: %w", err)
	}
	if changed != 1 {
		return ports.ErrClaimConflict
	}
	return nil
}

func (r *transactionRepository) CreateProgressEntry(ctx context.Context, entry work.ProgressEntry) error {
	completed, err := json.Marshal(entry.Completed)
	if err != nil {
		return err
	}
	remaining, err := json.Marshal(entry.Remaining)
	if err != nil {
		return err
	}
	discovered, err := json.Marshal(entry.Discovered)
	if err != nil {
		return err
	}
	blocker := []byte("null")
	if entry.Blocker != "" {
		blocker, err = json.Marshal(entry.Blocker)
		if err != nil {
			return err
		}
	}
	_, err = r.transaction.ExecContext(ctx, `
INSERT INTO progress_entries (id, work_item_id, actor_id, summary, completed_json, remaining_json, discovered_json, blocker_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, entry.ID, entry.WorkItemID, entry.ActorID, entry.Summary,
		string(completed), string(remaining), string(discovered), string(blocker), formatTime(entry.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert progress entry: %w", err)
	}
	return nil
}

func (r *transactionRepository) Artifacts(ctx context.Context, workItemID string) ([]output.Artifact, error) {
	return listWorkItemArtifacts(ctx, r.transaction, workItemID)
}

func listWorkItemArtifacts(ctx context.Context, reader sqlReader, workItemID string) ([]output.Artifact, error) {
	rows, err := reader.QueryContext(ctx, artifactSelect+" WHERE work_item_id = ? ORDER BY created_at, id", workItemID)
	if err != nil {
		return nil, fmt.Errorf("query artifacts: %w", err)
	}
	defer rows.Close()
	var artifacts []output.Artifact
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func listClaims(ctx context.Context, reader sqlReader, workItemID string) ([]work.Claim, error) {
	rows, err := reader.QueryContext(ctx, `
SELECT id, work_item_id, actor_id, acquired_at, expires_at, released_at, release_reason
FROM claims WHERE work_item_id = ? ORDER BY acquired_at, id`, workItemID)
	if err != nil {
		return nil, fmt.Errorf("query claims: %w", err)
	}
	defer rows.Close()
	var claims []work.Claim
	for rows.Next() {
		claim, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

func listProgressEntries(ctx context.Context, reader sqlReader, workItemID string) ([]work.ProgressEntry, error) {
	rows, err := reader.QueryContext(ctx, `
SELECT id, work_item_id, actor_id, summary, completed_json, remaining_json, discovered_json, blocker_json, created_at
FROM progress_entries WHERE work_item_id = ? ORDER BY created_at, id`, workItemID)
	if err != nil {
		return nil, fmt.Errorf("query progress entries: %w", err)
	}
	defer rows.Close()
	var entries []work.ProgressEntry
	for rows.Next() {
		entry, err := scanProgressEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (r *transactionRepository) IdempotencyRecord(ctx context.Context, actorID, key string) (ports.IdempotencyRecord, error) {
	return scanIdempotencyRecord(r.transaction.QueryRowContext(ctx, `
SELECT actor_id, key, operation, request_hash, response_json, created_at
FROM idempotency_records WHERE actor_id = ? AND key = ?`, actorID, key))
}

func (r *transactionRepository) CreateIdempotencyRecord(ctx context.Context, record ports.IdempotencyRecord) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO idempotency_records (actor_id, key, operation, request_hash, response_json, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, record.ActorID, record.Key, record.Operation, record.RequestHash, string(record.Response), formatTime(record.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert idempotency record: %w", err)
	}
	return nil
}

func (r *transactionRepository) LatestActivitySequence(ctx context.Context) (int64, error) {
	var sequence int64
	if err := r.transaction.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence), 0) FROM activity").Scan(&sequence); err != nil {
		return 0, fmt.Errorf("latest activity sequence: %w", err)
	}
	return sequence, nil
}

func (s *Store) IdempotencyRecord(ctx context.Context, actorID, key string) (ports.IdempotencyRecord, error) {
	return scanIdempotencyRecord(s.db.QueryRowContext(ctx, `SELECT actor_id, key, operation, request_hash, response_json, created_at FROM idempotency_records WHERE actor_id = ? AND key = ?`, actorID, key))
}

func scanActor(row scanner) (work.Actor, error) {
	var actor work.Actor
	var createdAt string
	if err := row.Scan(&actor.ID, &actor.Kind, &actor.DisplayName, &createdAt); err != nil {
		return work.Actor{}, err
	}
	var err error
	actor.CreatedAt, err = parseTime(createdAt)
	return actor, err
}

func scanClaim(row scanner) (work.Claim, error) {
	var claim work.Claim
	var acquiredAt, expiresAt string
	var releasedAt sql.NullString
	if err := row.Scan(&claim.ID, &claim.WorkItemID, &claim.ActorID, &acquiredAt, &expiresAt, &releasedAt, &claim.ReleaseReason); err != nil {
		return work.Claim{}, err
	}
	var err error
	claim.AcquiredAt, err = parseTime(acquiredAt)
	if err != nil {
		return work.Claim{}, err
	}
	claim.ExpiresAt, err = parseTime(expiresAt)
	if err != nil {
		return work.Claim{}, err
	}
	if releasedAt.Valid {
		claim.ReleasedAt, err = parseTime(releasedAt.String)
	}
	return claim, err
}

func scanProgressEntry(row scanner) (work.ProgressEntry, error) {
	var entry work.ProgressEntry
	var completed, remaining, discovered, blocker string
	var createdAt string
	if err := row.Scan(&entry.ID, &entry.WorkItemID, &entry.ActorID, &entry.Summary, &completed, &remaining, &discovered, &blocker, &createdAt); err != nil {
		return work.ProgressEntry{}, err
	}
	if err := json.Unmarshal([]byte(completed), &entry.Completed); err != nil {
		return work.ProgressEntry{}, fmt.Errorf("decode progress completed: %w", err)
	}
	if err := json.Unmarshal([]byte(remaining), &entry.Remaining); err != nil {
		return work.ProgressEntry{}, fmt.Errorf("decode progress remaining: %w", err)
	}
	if err := json.Unmarshal([]byte(discovered), &entry.Discovered); err != nil {
		return work.ProgressEntry{}, fmt.Errorf("decode progress discovered: %w", err)
	}
	if blocker != "null" {
		if err := json.Unmarshal([]byte(blocker), &entry.Blocker); err != nil {
			return work.ProgressEntry{}, fmt.Errorf("decode progress blocker: %w", err)
		}
	}
	var err error
	entry.CreatedAt, err = parseTime(createdAt)
	return entry, err
}

func scanIdempotencyRecord(row scanner) (ports.IdempotencyRecord, error) {
	var record ports.IdempotencyRecord
	var createdAt string
	if err := row.Scan(&record.ActorID, &record.Key, &record.Operation, &record.RequestHash, &record.Response, &createdAt); err != nil {
		return ports.IdempotencyRecord{}, mapNotFound(err)
	}
	var err error
	record.CreatedAt, err = parseTime(createdAt)
	return record, err
}
