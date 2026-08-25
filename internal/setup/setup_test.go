package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dennisschroeder/throughline/internal/clientconfig"
	"github.com/dennisschroeder/throughline/internal/daemon"
)

var _ daemon.ServiceManager = (*fakeManager)(nil)

type fakeManager struct {
	starts    int
	failStart bool
}

func (m *fakeManager) Start(context.Context) error {
	m.starts++
	if m.failStart {
		return errors.New("simulated start failure")
	}
	return nil
}
func (m *fakeManager) Stop(context.Context) error    { return nil }
func (m *fakeManager) Restart(context.Context) error { return nil }
func (m *fakeManager) Status(context.Context) (daemon.Status, error) {
	return daemon.Status{}, nil
}
func (m *fakeManager) Logs(context.Context, int) ([]string, error) { return nil, nil }

// writingReconciler always succeeds, writing a fixed marker into path.
func writingReconciler(marker string) func(string, clientconfig.Entry, bool) (clientconfig.Result, error) {
	return func(path string, entry clientconfig.Entry, force bool) (clientconfig.Result, error) {
		if err := os.WriteFile(path, []byte(marker+":"+entry.URL), 0o600); err != nil {
			return clientconfig.Result{}, err
		}
		return clientconfig.Result{Changed: true}, nil
	}
}

func failingReconciler(err error) func(string, clientconfig.Entry, bool) (clientconfig.Result, error) {
	return func(string, clientconfig.Entry, bool) (clientconfig.Result, error) {
		return clientconfig.Result{}, err
	}
}

func TestRunReconcilesEveryDetectedTargetAndStartsTheDaemon(t *testing.T) {
	directory := t.TempDir()
	credentialPath := filepath.Join(directory, "credentials")
	codexPath := filepath.Join(directory, "codex.toml")
	codexDetect := filepath.Join(directory, "codex-installed-marker")
	if err := os.WriteFile(codexDetect, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	manager := &fakeManager{}

	result, err := Run(context.Background(), Options{
		CredentialPath: credentialPath,
		BearerEnvVar:   "THROUGHLINE_MCP_TOKEN",
		DaemonURL:      "http://127.0.0.1:43121/mcp",
		Targets: []Target{
			{Name: "codex", ConfigPath: codexPath, DetectPath: codexDetect, ReconcileFn: writingReconciler("codex")},
		},
		Manager: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Token == "" || !result.TokenCreated {
		t.Fatalf("result token = %#v, want a freshly created token", result)
	}
	if !result.ServiceStarted || manager.starts != 1 {
		t.Fatalf("service not started: %#v", result)
	}
	if len(result.Targets) != 1 || !result.Targets[0].Changed || result.Targets[0].Skipped {
		t.Fatalf("target results = %#v", result.Targets)
	}
	content, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "codex:http://127.0.0.1:43121/mcp" {
		t.Fatalf("codex config = %q", content)
	}
}

func TestRunSkipsAnUndetectedHarness(t *testing.T) {
	directory := t.TempDir()
	result, err := Run(context.Background(), Options{
		CredentialPath: filepath.Join(directory, "credentials"),
		Targets: []Target{
			{Name: "hermes", ConfigPath: filepath.Join(directory, "hermes.yaml"), DetectPath: filepath.Join(directory, "not-installed"), ReconcileFn: writingReconciler("hermes")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Targets) != 1 || !result.Targets[0].Skipped {
		t.Fatalf("target results = %#v, want the undetected harness skipped", result.Targets)
	}
	if _, err := os.Stat(filepath.Join(directory, "hermes.yaml")); !os.IsNotExist(err) {
		t.Fatal("setup wrote a config file for an undetected harness")
	}
}

func TestRunReportsAConflictWithoutAbortingOtherTargets(t *testing.T) {
	directory := t.TempDir()
	claudePath := filepath.Join(directory, "claude.json")
	hermesPath := filepath.Join(directory, "hermes.yaml")
	if err := os.WriteFile(claudePath, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &fakeManager{}

	result, err := Run(context.Background(), Options{
		CredentialPath: filepath.Join(directory, "credentials"),
		Targets: []Target{
			{Name: "claude", ConfigPath: claudePath, ReconcileFn: failingReconciler(&clientconfig.ErrConflict{Path: claudePath, Reason: "test conflict"})},
			{Name: "hermes", ConfigPath: hermesPath, ReconcileFn: writingReconciler("hermes")},
		},
		Manager: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Targets) != 2 || !result.Targets[0].Conflict || !result.Targets[1].Changed {
		t.Fatalf("target results = %#v", result.Targets)
	}
	// The conflicting target is left exactly as it was; setup never overwrites it silently.
	content, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Fatalf("claude config = %q, want it untouched", content)
	}
	if manager.starts != 1 {
		t.Fatal("a per-target conflict should not prevent the daemon from starting")
	}
}

func TestRunRollsBackEveryTargetWhenTheServiceFailsToStart(t *testing.T) {
	directory := t.TempDir()
	codexPath := filepath.Join(directory, "codex.toml")
	claudePath := filepath.Join(directory, "claude.json")
	if err := os.WriteFile(codexPath, []byte("original codex"), 0o600); err != nil {
		t.Fatal(err)
	}
	// claude.json does not exist yet: a successful reconcile creates it, and rollback must
	// remove it again rather than leave a half-applied fresh file behind.
	manager := &fakeManager{failStart: true}

	_, err := Run(context.Background(), Options{
		CredentialPath: filepath.Join(directory, "credentials"),
		Targets: []Target{
			{Name: "codex", ConfigPath: codexPath, ReconcileFn: writingReconciler("codex")},
			{Name: "claude", ConfigPath: claudePath, ReconcileFn: writingReconciler("claude")},
		},
		Manager: manager,
	})
	if err == nil {
		t.Fatal("expected an error when the daemon fails to start")
	}

	content, readErr := os.ReadFile(codexPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "original codex" {
		t.Fatalf("codex config after rollback = %q, want the original content restored", content)
	}
	if _, statErr := os.Stat(claudePath); !os.IsNotExist(statErr) {
		t.Fatal("claude config should have been removed by rollback since it did not exist before setup")
	}
}

func TestRunRollsBackOnAGenuineReconcileError(t *testing.T) {
	directory := t.TempDir()
	codexPath := filepath.Join(directory, "codex.toml")
	claudePath := filepath.Join(directory, "claude.json")
	if err := os.WriteFile(codexPath, []byte("original codex"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &fakeManager{}

	_, err := Run(context.Background(), Options{
		CredentialPath: filepath.Join(directory, "credentials"),
		Targets: []Target{
			{Name: "codex", ConfigPath: codexPath, ReconcileFn: writingReconciler("codex")},
			{Name: "claude", ConfigPath: claudePath, ReconcileFn: failingReconciler(errors.New("disk exploded"))},
		},
		Manager: manager,
	})
	if err == nil {
		t.Fatal("expected an error from the genuine (non-conflict) reconcile failure")
	}
	content, readErr := os.ReadFile(codexPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "original codex" {
		t.Fatalf("codex config after rollback = %q, want the original content restored", content)
	}
	if manager.starts != 0 {
		t.Fatal("the daemon should never start after a rolled-back setup")
	}
}
