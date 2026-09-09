package ports

// Effect identifies one entity changed by a committed mutation. Version is the
// version the transaction committed for it; append-only records carry one.
//
// An Effect says that an entity changed, not how. A deleted entity appears with
// the last version it actually had, so a consumer must treat an Effect as an
// invalidation, never as evidence that the entity is still there.
type Effect struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}
