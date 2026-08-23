package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dennisschroeder/workgraph/internal/ports"
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
	response, err := json.Marshal(idempotencyResponse[T]{Result: result, ChangeCursor: cursor})
	if err != nil {
		return zero, fmt.Errorf("encode idempotency response: %w", err)
	}
	if err := repository.CreateIdempotencyRecord(ctx, ports.IdempotencyRecord{
		ActorID: actorID, Key: key, Operation: operation, RequestHash: requestHash,
		Response: response, CreatedAt: service.clock.Now(),
	}); err != nil {
		return zero, err
	}
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
	Result       T     `json:"result"`
	ChangeCursor int64 `json:"change_cursor"`
}
