package dashboard

import (
	"fmt"
	"time"

	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// ageLabel renders the duration since t as the short monospace form the spec uses
// throughout ("waiting <age>", "last call <age>", "dormant <age>"): seconds under a
// minute, minutes under an hour, hours under a day, otherwise days.
func ageLabel(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// actorLiveness is the last-tool-call-per-actor fact the whole snapshot derives liveness
// from. work.Actor carries no LastSeenAt field (see investigation), so this is built once
// per request from the most recent work.Activity row per ActorID — exactly the fallback the
// task brief allows ("derive it from the most recent Activity row per actor").
type actorLiveness struct {
	lastCallAt map[string]time.Time
	now        time.Time
}

func buildActorLiveness(activity []work.Activity, now time.Time) *actorLiveness {
	al := &actorLiveness{lastCallAt: make(map[string]time.Time), now: now}
	for _, a := range activity {
		if a.ActorID == "" {
			continue
		}
		if existing, ok := al.lastCallAt[a.ActorID]; !ok || a.CreatedAt.After(existing) {
			al.lastCallAt[a.ActorID] = a.CreatedAt
		}
	}
	return al
}

func (al *actorLiveness) ref(actorID string) ActorRef {
	ref := ActorRef{ID: actorID}
	last, ok := al.lastCallAt[actorID]
	if !ok {
		return ref
	}
	ref.LastCallAt = last.UTC().Format(time.RFC3339)
	ref.Live = al.now.Sub(last) <= dormantThresholdSeconds*time.Second
	return ref
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (al *actorLiveness) live(actorID string) bool {
	last, ok := al.lastCallAt[actorID]
	if !ok {
		return false
	}
	return al.now.Sub(last) <= dormantThresholdSeconds*time.Second
}
