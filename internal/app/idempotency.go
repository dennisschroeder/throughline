package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

func executeIdempotently[T any](ctx context.Context, service *Service, repository ports.Repository, actorID, key, operation string, request any, execute func() (T, error)) (T, error) {
	var zero T
	actorID, key, requestHash, err := idempotencyRequest(actorID, key, request)
	if err != nil {
		return zero, err
	}
	existing, err := repository.IdempotencyRecord(ctx, actorID, key)
	if err == nil {
		if existing.Operation != operation || existing.RequestHash != requestHash {
			return zero, ports.ErrIdempotencyMismatch
		}
		var replay idempotencyResponse[T]
		if err := json.Unmarshal(existing.Response, &replay); err != nil {
			return zero, fmt.Errorf("decode idempotency replay: %w", err)
		}
		if replay.Effects == nil {
			// Responses written before REP-02 have no effects member. Upgrade the
			// in-memory replay explicitly from its durable result so a restart does
			// not silently turn a valid replay into an empty mutation contract.
			replay.Effects = legacyEffects(operation, replay.Result)
			if len(replay.Effects) == 0 {
				return zero, ErrLegacyIdempotencyReplay
			}
		}
		captureMutation(ctx, replay.Effects)
		return replay.Result, nil
	}
	if !errors.Is(err, ports.ErrNotFound) {
		return zero, err
	}
	result, err := execute()
	if err != nil {
		return zero, err
	}
	cursor, err := repository.LatestActivitySequence(ctx)
	if err != nil {
		return zero, fmt.Errorf("read idempotency cursor: %w", err)
	}
	effects, err := mutationEffects(ctx, repository, result)
	if err != nil {
		return zero, fmt.Errorf("collect mutation effects: %w", err)
	}
	response, err := json.Marshal(idempotencyResponse[T]{Result: result, ChangeCursor: cursor, Effects: effects})
	if err != nil {
		return zero, fmt.Errorf("encode idempotency response: %w", err)
	}
	if err := repository.CreateIdempotencyRecord(ctx, ports.IdempotencyRecord{
		ActorID: actorID, Key: key, Operation: operation, RequestHash: requestHash,
		Response: response, CreatedAt: service.clock.Now(),
	}); err != nil {
		return zero, err
	}
	captureMutation(ctx, effects)
	return result, nil
}

// replayIdempotently checks for an existing durable response before a mutation
// allocates IDs. executeIdempotently repeats the check inside the write transaction.
func replayIdempotently[T any](ctx context.Context, service *Service, actorID, key, operation string, request any) (T, bool, error) {
	var zero T
	actorID, key, requestHash, err := idempotencyRequest(actorID, key, request)
	if err != nil {
		return zero, false, err
	}
	var result T
	found := false
	err = service.store.WithinTransaction(ctx, func(repository ports.Repository) error {
		existing, err := repository.IdempotencyRecord(ctx, actorID, key)
		if errors.Is(err, ports.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if existing.Operation != operation || existing.RequestHash != requestHash {
			return ports.ErrIdempotencyMismatch
		}
		var replay idempotencyResponse[T]
		if err := json.Unmarshal(existing.Response, &replay); err != nil {
			return fmt.Errorf("decode idempotency replay: %w", err)
		}
		if replay.Effects == nil {
			replay.Effects = legacyEffects(operation, replay.Result)
			if len(replay.Effects) == 0 {
				return ErrLegacyIdempotencyReplay
			}
		}
		captureMutation(ctx, replay.Effects)
		result = replay.Result
		found = true
		return nil
	})
	if err != nil {
		return zero, false, err
	}
	return result, found, nil
}

func idempotencyRequest(actorID, key string, request any) (string, string, string, error) {
	actorID = strings.TrimSpace(actorID)
	key = strings.TrimSpace(key)
	if actorID == "" {
		return "", "", "", errors.New("mutation requires an actor id")
	}
	if key == "" {
		return "", "", "", errors.New("mutation requires an idempotency key")
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return "", "", "", fmt.Errorf("encode idempotency request: %w", err)
	}
	digest := sha256.Sum256(requestJSON)
	return actorID, key, hex.EncodeToString(digest[:]), nil
}

type idempotencyResponse[T any] struct {
	Result       T        `json:"result"`
	ChangeCursor int64    `json:"change_cursor"`
	Effects      []Effect `json:"effects"`
}

// Effect is part of the application contract. It is an alias so transport
// adapters and repositories cannot invent a second representation.
type Effect = ports.Effect

// ErrLegacyIdempotencyReplay reports a stored response from before REP-02 whose
// effects cannot be reconstructed exactly. Replaying it is refused rather than
// answered with a guess, and the mutation is never executed a second time.
var ErrLegacyIdempotencyReplay = errors.New("idempotency replay predates mutation effects and cannot be upgraded")

func mutationEffects(ctx context.Context, repository ports.Repository, result any) ([]Effect, error) {
	if collector, ok := repository.(mutationEffectCollector); ok {
		effects, err := collector.CollectEffects(ctx)
		if effects == nil && err == nil {
			effects = []Effect{}
		}
		return effects, err
	}
	return effectsFromResult(result), nil
}

// legacyEffects reconstructs the effect set of a pre-REP-02 response. Only
// operations whose whole write touches exactly the entity they return are
// listed: anything that also supersedes a predecessor, accepts sibling work,
// or moves a second aggregate cannot be rebuilt from the stored result and
// must fail instead.
func legacyEffects(operation string, result any) []Effect {
	var reconstructed []Effect
	switch operation {
	case "register_actor", "create_objective", "patch_objective", "transition_objective",
		"transition_context", "ask_question", "answer_question", "waive_question",
		"create_plan", "propose_output_profile", "patch_external_action_metadata":
		reconstructed = effectsFromResult(result)
	case "record_context":
		// Recording a context record that supersedes a predecessor also rewrites
		// that predecessor, and its committed version is not derivable from the
		// stored result. Only the plain case is reconstructible.
		if record, ok := result.(work.ContextRecord); ok && record.SupersedesID != "" {
			return nil
		}
		reconstructed = effectsFromResult(result)
	default:
		return nil
	}
	// A stored response predates whatever the binary that wrote it knew about.
	// Where the version field itself is newer than the response, decoding leaves
	// it at zero, and a zero version is not a version this contract may report.
	// Refusing is the same answer the contract gives for everything else it
	// cannot rebuild exactly.
	for _, effect := range reconstructed {
		if effect.Version < 1 {
			return nil
		}
	}
	return reconstructed
}

func effectsFromResult(result any) []Effect {
	switch value := result.(type) {
	case work.Objective:
		return []Effect{{Kind: "objective", ID: value.ID, Version: value.Version}}
	case work.Plan:
		return []Effect{{Kind: "plan", ID: value.ID, Version: value.Version}}
	case work.WorkItem:
		return []Effect{{Kind: "work_item", ID: value.ID, Version: value.Version}}
	case work.ContextRecord:
		return []Effect{{Kind: "context_record", ID: value.ID, Version: value.Version}}
	case work.Question:
		return []Effect{{Kind: "question", ID: value.ID, Version: value.Version}}
	case work.Decision:
		return []Effect{{Kind: "decision", ID: value.ID, Version: value.Version}}
	case work.Approval:
		return []Effect{{Kind: "approval", ID: value.ID, Version: value.Version}}
	case work.Actor:
		return []Effect{{Kind: "actor", ID: value.ID, Version: value.Version}}
	case work.Dependency:
		return []Effect{{Kind: "dependency", ID: value.ID, Version: value.Version}}
	case output.Profile:
		return []Effect{{Kind: "output_profile", ID: value.ID, Version: value.StateVersion}}
	case output.ExpectedOutput:
		return []Effect{{Kind: "expected_output", ID: value.ID, Version: value.Version}}
	case output.OutputRevision:
		return []Effect{{Kind: "output_revision", ID: value.ID, Version: value.StateVersion}}
	case output.OutputRequirement:
		return []Effect{{Kind: "output_requirement", ID: value.ID, Version: value.Version}}
	case output.Artifact:
		return []Effect{{Kind: "artifact", ID: value.ID, Version: value.Version}}
	case authority.ExternalAction:
		return []Effect{{Kind: "external_action", ID: value.ID, Version: value.Version}}
	case authority.ActionApproval:
		return []Effect{{Kind: "action_approval", ID: value.ID, Version: value.Version}}
	case authority.ExternalActionExecution:
		return []Effect{{Kind: "external_action_execution", ID: value.ID, Version: value.Version}}
	}
	return nil
}
