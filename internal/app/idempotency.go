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
	actorID = strings.TrimSpace(actorID)
	key = strings.TrimSpace(key)
	if actorID == "" || key == "" {
		return zero, errors.New("mutation requires actor id and idempotency key")
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return zero, fmt.Errorf("encode idempotency request: %w", err)
	}
	digest := sha256.Sum256(requestJSON)
	requestHash := hex.EncodeToString(digest[:])
	existing, err := repository.IdempotencyRecord(ctx, actorID, key)
	if err == nil {
		if existing.Operation != operation || existing.RequestHash != requestHash {
			return zero, ports.ErrIdempotencyMismatch
		}
		var replay T
		if err := json.Unmarshal(existing.Response, &replay); err != nil {
			return zero, fmt.Errorf("decode idempotency replay: %w", err)
		}
		return replay, nil
	}
	if !errors.Is(err, ports.ErrNotFound) {
		return zero, err
	}
	result, err := execute()
	if err != nil {
		return zero, err
	}
	response, err := json.Marshal(result)
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
