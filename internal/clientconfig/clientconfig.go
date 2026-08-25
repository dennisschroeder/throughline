// Package clientconfig defines the shared reconciliation contract every managed-harness
// global-configuration adapter (Codex, Claude Code, Hermes) implements: write or update one
// named MCP server entry pointing at the loopback daemon, preserving every other key in the
// file, and refusing to silently overwrite a conflicting entry it did not itself write.
package clientconfig

import "fmt"

// ServerName is the one MCP server name every adapter registers under, matching across all
// three harnesses so diagnostics and documentation can refer to "the throughline entry"
// without a per-client name to track.
const ServerName = "throughline"

// BearerEnvVar is the one environment variable name every managed client configuration
// references for the daemon's bearer credential, matching
// docs/research/mcp-transport-compatibility.md's common-denominator contract.
const BearerEnvVar = "THROUGHLINE_MCP_TOKEN"

// Entry is the provider-neutral shape every adapter reconciles into its own file format.
// BearerTokenEnvVar names the environment variable the harness should read the credential
// from; the adapter never writes the token's actual value into a client configuration file.
type Entry struct {
	URL               string
	BearerTokenEnvVar string
	Required          bool
}

// ErrConflict is returned when the existing configuration has an entry under ServerName
// that this adapter did not write (a different url, a different token source, or content
// in a shape the adapter does not recognize as its own). Callers must not overwrite it
// without explicit, separately confirmed instruction; Reconcile never does so itself.
type ErrConflict struct {
	Path   string
	Reason string
}

func (e *ErrConflict) Error() string {
	return fmt.Sprintf("existing %s entry in %s does not match the managed configuration: %s", ServerName, e.Path, e.Reason)
}

// Result reports what a Reconcile call did, so callers (and their tests) can distinguish a
// fresh write from a no-op from a diagnosed conflict without parsing the error string.
type Result struct {
	// Changed is true only when the file was actually written.
	Changed bool
}
