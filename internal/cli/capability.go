package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	throughlinesqlite "github.com/dennisschroeder/throughline/internal/sqlite"
)

// The grant retries while another writer holds the lock; variables so tests
// can exhaust the budget without waiting it out.
var (
	capabilityGrantBusyRetries = 50
	capabilityGrantBusyBackoff = 100 * time.Millisecond
)

// runCapabilityGrant assigns a capability as a human principal. It is a CLI
// command and deliberately not an MCP tool: over MCP the only principal is the
// agent itself, which could then grant itself whatever a claim requires. That
// is also why it is the one domain command that opens a workspace database
// instead of calling the daemon (ADR 0025 records the exception). It never
// migrates; a schema mismatch fails with the update-and-restart remediation.
// --as names the principal on the local machine's trust; it does not
// authenticate one.
func runCapabilityGrant(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("capability grant", flag.ContinueOnError)
	flags.SetOutput(stderr)
	actorID := flags.String("actor", "", "actor_id that receives the capability (required)")
	slug := flags.String("capability", "", "capability slug to grant (required)")
	granter := flags.String("as", "", "registered human actor_id granting it (required)")
	description := flags.String("description", "", "description recorded for a capability that does not exist yet")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("expected at most one workspace directory")
	}
	if strings.TrimSpace(*actorID) == "" || strings.TrimSpace(*slug) == "" || strings.TrimSpace(*granter) == "" {
		return errors.New("--actor, --capability and --as are required")
	}
	workspace, err := findWorkspace(optionalDirectory(flags.Args()))
	if err != nil {
		return err
	}
	// Opening creates a missing file, and an empty database would then read as
	// merely unmigrated, sending the user to restart a daemon that would
	// migrate the empty file and hide the missing data.
	if _, err := os.Stat(workspace.DatabasePath); err != nil {
		return fmt.Errorf("workspace database %s is not readable: %w", workspace.DatabasePath, err)
	}
	database, err := throughlinesqlite.Open(ctx, workspace.DatabasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.CheckSchemaCurrent(ctx); err != nil {
		return err
	}
	ids := app.UUIDv7Generator{}
	key, err := ids.New()
	if err != nil {
		return fmt.Errorf("generate idempotency key: %w", err)
	}
	service := app.NewService(database.Store(), ids, app.SystemClock{})
	command := app.AssignActorCapabilityCommand{
		ActorID: *actorID, Capability: *slug, Description: *description, GrantedBy: *granter, IdempotencyKey: "cli-capability-grant-" + key,
	}
	// The daemon may hold the write lock. A deferred transaction that finds it
	// taken fails at once instead of waiting out the busy timeout, so the grant
	// retries briefly; the same key makes a retry of a committed grant a replay.
	var granted app.ActorCapability
	for attempt := 0; ; attempt++ {
		granted, err = app.UnwrapMutation(service.AssignActorCapability(ctx, command))
		if err == nil || !throughlinesqlite.IsBusy(err) || attempt == capabilityGrantBusyRetries {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("capability grant interrupted while the workspace database was locked: %w", ctx.Err())
		case <-time.After(capabilityGrantBusyBackoff):
		}
	}
	if err != nil {
		if throughlinesqlite.IsBusy(err) {
			return fmt.Errorf("the workspace database stayed locked by another writer, most likely the daemon; retry shortly: %w", err)
		}
		return err
	}
	fmt.Fprintf(stdout, "granted capability %s to %s as %s\n", granted.Capability.Slug, granted.ActorID, strings.TrimSpace(*granter))
	return nil
}
