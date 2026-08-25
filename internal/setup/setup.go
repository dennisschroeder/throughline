// Package setup implements throughline setup's atomic preflight/reconcile/backup/rollback
// sequence: generate or load the daemon credential, reconcile every detected harness's
// global MCP configuration, and start the managed daemon service — leaving every file
// exactly as it was if any step fails.
package setup

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dennisschroeder/throughline/internal/clientconfig"
	"github.com/dennisschroeder/throughline/internal/credential"
	"github.com/dennisschroeder/throughline/internal/daemon"
)

// Target is one harness's global configuration file and the reconciler that knows its
// format. DetectPath, if non-empty, gates reconciliation on that path already existing
// (setup only touches a harness that appears to be installed); an empty DetectPath always
// reconciles.
type Target struct {
	Name        string
	ConfigPath  string
	DetectPath  string
	ReconcileFn func(path string, entry clientconfig.Entry, force bool) (clientconfig.Result, error)
}

// Options configures one setup run.
type Options struct {
	CredentialPath string
	BearerEnvVar   string
	DaemonURL      string
	Targets        []Target
	Manager        daemon.ServiceManager
	Force          bool
}

// TargetResult reports what happened to one harness's configuration.
type TargetResult struct {
	Name     string
	Skipped  bool // the harness was not detected as installed
	Changed  bool
	Conflict bool
	Err      error
}

// Result is the full outcome of one setup run.
type Result struct {
	Token          string
	TokenCreated   bool
	ServiceStarted bool
	Targets        []TargetResult
}

// Run performs the accepted sequence: preflight (nothing destructive happens before every
// step is validated as attemptable), reconcile every detected target with a backup taken
// first, start the managed service, and roll every backed-up file back to its prior content
// if any reconciliation with a genuine error (not a diagnosed, skippable conflict) or the
// service start fails. A conflict on one target does not abort the others; it is reported
// in that target's TargetResult and setup continues, since Force lets a caller resolve
// conflicts target-by-target rather than needing every harness to agree at once.
func Run(ctx context.Context, opts Options) (Result, error) {
	tokenExisted := fileExists(opts.CredentialPath)
	token, err := credential.LoadOrCreate(opts.CredentialPath)
	if err != nil {
		return Result{}, fmt.Errorf("setup preflight: %w", err)
	}
	result := Result{Token: token, TokenCreated: !tokenExisted}

	entry := clientconfig.Entry{URL: opts.DaemonURL, BearerTokenEnvVar: opts.BearerEnvVar, Required: true}

	var backups []backup
	rollback := func() {
		for _, b := range backups {
			_ = b.restore()
		}
	}

	for _, target := range opts.Targets {
		if target.DetectPath != "" && !fileExists(target.DetectPath) {
			result.Targets = append(result.Targets, TargetResult{Name: target.Name, Skipped: true})
			continue
		}
		b, err := newBackup(target.ConfigPath)
		if err != nil {
			rollback()
			return Result{}, fmt.Errorf("setup backup %s: %w", target.Name, err)
		}
		backups = append(backups, b)

		reconcileResult, err := target.ReconcileFn(target.ConfigPath, entry, opts.Force)
		if err != nil {
			var conflict *clientconfig.ErrConflict
			if errors.As(err, &conflict) {
				result.Targets = append(result.Targets, TargetResult{Name: target.Name, Conflict: true, Err: err})
				continue
			}
			rollback()
			return Result{}, fmt.Errorf("setup reconcile %s: %w", target.Name, err)
		}
		result.Targets = append(result.Targets, TargetResult{Name: target.Name, Changed: reconcileResult.Changed})
	}

	if opts.Manager != nil {
		if err := opts.Manager.Start(ctx); err != nil {
			rollback()
			return Result{}, fmt.Errorf("setup start daemon: %w", err)
		}
		result.ServiceStarted = true
	}

	return result, nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

type backup struct {
	path    string
	existed bool
	content []byte
}

func newBackup(path string) (backup, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return backup{path: path, existed: false}, nil
		}
		return backup{}, err
	}
	return backup{path: path, existed: true, content: content}, nil
}

func (b backup) restore() error {
	if !b.existed {
		return os.Remove(b.path)
	}
	return os.WriteFile(b.path, b.content, 0o600)
}
