package daemonhttp

import (
	"crypto/rand"
	"encoding/hex"
)

// newRequestID returns a short random correlation id for one HTTP request, distinct from
// MCP-level idempotency keys.
func newRequestID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(buffer)
}
