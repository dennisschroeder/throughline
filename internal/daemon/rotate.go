package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dennisschroeder/throughline/internal/credential"
)

// RotateCredential regenerates the daemon's bearer token: it preflights (the current
// credential must be readable), backs it up, commits a fresh token, restarts the daemon
// through manager so it starts checking the new token, and — if the restart or the caller's
// post-restart verify fails — restores the backup and restarts again so the daemon is left
// serving a token that is known to work. Updating managed client configurations (Codex,
// Claude Code, Hermes) with the new token is not this function's responsibility; a later
// work item extends rotation to also rewrite those once their adapters exist.
func RotateCredential(ctx context.Context, credentialPath string, manager ServiceManager, verify func(context.Context) error) (newToken string, err error) {
	original, err := os.ReadFile(credentialPath)
	if err != nil {
		return "", fmt.Errorf("rotation preflight: read current credential: %w", err)
	}
	backupPath := credentialPath + ".backup"
	if err := os.WriteFile(backupPath, original, 0o600); err != nil {
		return "", fmt.Errorf("rotation backup: %w", err)
	}

	newToken, err = credential.Regenerate(credentialPath)
	if err != nil {
		return "", fmt.Errorf("rotation commit: %w", err)
	}

	if err := manager.Restart(ctx); err != nil {
		return "", rollback(ctx, credentialPath, backupPath, original, manager, fmt.Errorf("rotation restart: %w", err))
	}
	if verify != nil {
		if err := verify(ctx); err != nil {
			return "", rollback(ctx, credentialPath, backupPath, original, manager, fmt.Errorf("rotation verification: %w", err))
		}
	}

	_ = os.Remove(backupPath)
	return newToken, nil
}

// rollback restores the pre-rotation credential, restarts the daemon so it is serving that
// restored token again, and wraps cause with whatever went wrong along the way so the
// caller sees the original failure plus rollback status, never a silently swallowed one.
func rollback(ctx context.Context, credentialPath, backupPath string, original []byte, manager ServiceManager, cause error) error {
	restoreErr := os.WriteFile(credentialPath, original, 0o600)
	_ = os.Remove(backupPath)
	restartErr := manager.Restart(ctx)
	switch {
	case restoreErr != nil:
		return errors.Join(cause, fmt.Errorf("rollback restore failed: %w", restoreErr))
	case restartErr != nil:
		return errors.Join(cause, fmt.Errorf("rollback restart failed: %w", restartErr))
	default:
		return fmt.Errorf("%w (rolled back to the previous credential)", cause)
	}
}
