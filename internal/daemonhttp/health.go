package daemonhttp

import (
	"encoding/json"
	"net/http"
)

// HealthResponse is the stable, minimal authenticated health contract. It never includes a
// filesystem path, registry entry, or provider detail — only enough to confirm the daemon
// is this version and is up.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// HealthHandler serves HealthResponse. It carries no workspace or registry state; callers
// wrap it with Protect so it is authenticated like every other daemon endpoint.
func HealthHandler(version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok", Version: version})
	})
}
