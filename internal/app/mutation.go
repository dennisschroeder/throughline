package app

import (
	"context"
	"errors"
)

// Mutation is the transport-independent result of a committed write. Effects
// are part of the application contract and are collected before its transaction
// closes, so adapters never need to infer collateral changes from a response.
type Mutation[T any] struct {
	Result  T        `json:"result"`
	Effects []Effect `json:"effects"`
}

type mutationEffectCollector interface {
	CollectEffects(context.Context) ([]Effect, error)
}

func (mutation Mutation[T]) MutationResult() any { return mutation.Result }

func (mutation Mutation[T]) MutationEffects() []Effect { return mutation.Effects }

type MutationEnvelope interface {
	MutationResult() any
	MutationEffects() []Effect
}

// UnwrapMutation is for in-process consumers that do not expose mutation
// receipts. Transports should preserve Effects in their public response.
func UnwrapMutation[T any](mutation Mutation[T], err error) (T, error) {
	return mutation.Result, err
}

type mutationCaptureKey struct{}

type mutationCapture struct {
	effects []Effect
	set     bool
}

func beginMutation(ctx context.Context) (context.Context, *mutationCapture) {
	capture := &mutationCapture{}
	return context.WithValue(ctx, mutationCaptureKey{}, capture), capture
}

func captureMutation(ctx context.Context, effects []Effect) {
	if capture, ok := ctx.Value(mutationCaptureKey{}).(*mutationCapture); ok {
		capture.effects = append([]Effect(nil), effects...)
		if capture.effects == nil {
			capture.effects = []Effect{}
		}
		capture.set = true
	}
}

func finishMutation[T any](capture *mutationCapture, result T, err error) (Mutation[T], error) {
	if err != nil {
		return Mutation[T]{}, err
	}
	if capture == nil || !capture.set {
		return Mutation[T]{}, errors.New("mutation completed without a captured effect set")
	}
	return Mutation[T]{Result: result, Effects: capture.effects}, nil
}
