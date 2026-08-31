package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/claudecodeconfig"
	"github.com/dennisschroeder/throughline/internal/clientconfig"
	"github.com/dennisschroeder/throughline/internal/codexconfig"
	"github.com/dennisschroeder/throughline/internal/config"
	"github.com/dennisschroeder/throughline/internal/credential"
	"github.com/dennisschroeder/throughline/internal/daemon"
	"github.com/dennisschroeder/throughline/internal/daemonhttp"
	"github.com/dennisschroeder/throughline/internal/dashboard"
	"github.com/dennisschroeder/throughline/internal/hermesconfig"
	"github.com/dennisschroeder/throughline/internal/launchd"
	throughlinemcp "github.com/dennisschroeder/throughline/internal/mcp"
	"github.com/dennisschroeder/throughline/internal/registry"
	"github.com/dennisschroeder/throughline/internal/router"
	"github.com/dennisschroeder/throughline/internal/setup"
	throughlinesqlite "github.com/dennisschroeder/throughline/internal/sqlite"
	"github.com/dennisschroeder/throughline/internal/systemd"
)

// registryPathForTesting overrides the registry location for hermetic tests, matching the
// accepted decision that production has no alternate registry-location routing mechanism
// and only tests may inject a path. It is set directly by tests in this package.
var registryPathForTesting string

func registryPath() (string, error) {
	if registryPathForTesting != "" {
		return registryPathForTesting, nil
	}
	return registry.DefaultPath()
}

func openRegistry(ctx context.Context) (*registry.Registry, error) {
	path, err := registryPath()
	if err != nil {
		return nil, err
	}
	return registry.Open(ctx, path)
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeUsage(stderr)
		return 2
	}
	switch args[0] {
	case "init":
		if err := runInit(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "throughline init: %v\n", err)
			return 1
		}
		return 0
	case "unregister":
		if err := runUnregister(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "throughline unregister: %v\n", err)
			return 1
		}
		return 0
	case "ready":
		if err := runReady(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "throughline ready: %v\n", err)
			return 1
		}
		return 0
	case "show":
		if err := runShow(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "throughline show: %v\n", err)
			return 1
		}
		return 0
	case "mcp":
		if err := runMCP(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "throughline mcp: %v\n", err)
			return 1
		}
		return 0
	case "setup":
		if err := runSetup(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "throughline setup: %v\n", err)
			return 1
		}
		return 0
	case "uninstall":
		if err := runUninstall(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "throughline uninstall: %v\n", err)
			return 1
		}
		return 0
	case "doctor":
		if err := runDoctor(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "throughline doctor: %v\n", err)
			return 1
		}
		return 0
	case "daemon":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "usage: throughline daemon <start|stop|restart|status|logs|rotate-credential> [arguments]")
			return 2
		}
		if err := runDaemon(ctx, args[1], args[2:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "throughline daemon %s: %v\n", args[1], err)
			return 1
		}
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, versionLine())
		return 0
	case "help", "-h", "--help":
		writeUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		writeUsage(stderr)
		return 2
	}
}

// defaultAddr is the loopback address throughline mcp and doctor/daemon status agree on
// absent an explicit --addr; it is not otherwise discoverable or configurable per-workspace.
const defaultAddr = "127.0.0.1:43121"

// credentialPathForTesting overrides the credential location for hermetic tests, mirroring
// registryPathForTesting: production has no alternate credential-location routing
// mechanism.
var credentialPathForTesting string

func credentialPath() (string, error) {
	if credentialPathForTesting != "" {
		return credentialPathForTesting, nil
	}
	return credential.DefaultPath()
}

// runMCP serves every workspace over one authenticated Streamable HTTP endpoint, resolving
// workspace_id per request through the WorkspaceRouter. There is no per-workspace argument:
// this is the single global daemon transport, not a process bound to one workspace. It
// blocks until ctx is cancelled or the listener fails; process supervision
// (launchd/systemd, auto-restart) is a later work item's responsibility, not this command's.
func runMCP(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", defaultAddr, "loopback address to serve Streamable HTTP on")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("mcp takes no positional arguments; workspace_id is resolved per request")
	}
	allowedHosts, err := loopbackHosts(*addr)
	if err != nil {
		return err
	}

	stateDir, err := daemonStateDir()
	if err != nil {
		return err
	}
	lock, err := daemon.Acquire(filepath.Join(stateDir, "daemon.lock"))
	if err != nil {
		return err
	}
	defer lock.Release()

	tokenPath, err := credentialPath()
	if err != nil {
		return err
	}
	token, err := credential.LoadOrCreate(tokenPath)
	if err != nil {
		return err
	}

	registryHandle, err := openRegistry(ctx)
	if err != nil {
		return err
	}
	defer registryHandle.Close()

	workspaceRouter := router.New(registryHandle, router.NewProviderManager(router.SQLiteProvider{}), app.UUIDv7Generator{}, app.SystemClock{}, 0)
	defer workspaceRouter.Close()

	resolvedVersion, _, _ := versionInfo()

	// dashboardHub is the invalidation hub the MCP write path (via HandlerWithHub) fires
	// into after every workspace-scoped write commits, and that the dashboard SSE handler
	// subscribes to for push updates. See internal/dashboard for the read-only live
	// dashboard surface this daemon also serves.
	dashboardHub := dashboard.NewHub()
	dashboardHandlers := dashboard.NewHandlers(dashboard.Config{
		Router:       workspaceRouter,
		Hub:          dashboardHub,
		AllowedHosts: allowedHosts,
		Logger:       slog.New(slog.NewJSONHandler(stderr, nil)),
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", throughlinemcp.HandlerWithHub(workspaceRouter, dashboardHub))
	mux.Handle("/health", daemonhttp.HealthHandler(resolvedVersion))
	// /dashboard/token mints a single-use dashboard login link; it is intentionally left
	// under the bearer-token boundary below, exactly like /mcp, since only an
	// already-authenticated MCP client may mint one.
	mux.Handle("/dashboard/token", dashboardHandlers.MintLoginTokenHandler())
	protected := daemonhttp.Protect(daemonhttp.Config{
		Token:        token,
		AllowedHosts: allowedHosts,
		Logger:       slog.New(slog.NewJSONHandler(stderr, nil)),
	}, mux)

	// The dashboard's browser-facing routes cannot present a bearer token (a browser tab
	// has none), so they sit outside the bearer boundary and authenticate instead via the
	// session cookie ExchangeLoginTokenHandler sets — see internal/dashboard's own
	// host/origin check, which still restricts them to loopback. Go's ServeMux prefers the
	// more specific pattern, so these routes take precedence over the "/" catch-all into
	// the bearer-protected mux above. /dashboard/token is registered here too, pointing
	// back at protected: without it, the "/dashboard/" subtree pattern below (registered
	// for the static page) would shadow it, since a subtree pattern outranks "/" for any
	// path under it, bearer-protected or not.
	top := http.NewServeMux()
	top.Handle("/", protected)
	top.Handle("/dashboard/token", protected)
	top.Handle("/dashboard/login", dashboardHandlers.ExchangeLoginTokenHandler())
	// Iteration 5: the browser-facing API moved from an SSE push stream to the spec's
	// poll model (GET /api/v1/changes every 2s, reload the snapshot only when the cursor
	// moved) and from a single ad hoc snapshot to one view-model payload per region. All
	// of these are GET-only per ADR 0027 — the dashboard reads through the same Router
	// seam as MCP, calls no write/mutation application-service method, and never mutates.
	top.Handle("/dashboard/api/v1/objectives", dashboardHandlers.ObjectivesHandler())
	top.Handle("/dashboard/api/v1/loop", dashboardHandlers.LoopHandler())
	top.Handle("/dashboard/api/v1/changes", dashboardHandlers.ChangesHandler())
	top.Handle("/dashboard/api/v1/gate", dashboardHandlers.GateDetailHandler())
	top.Handle("/dashboard/api/v1/item", dashboardHandlers.ItemDetailHandler())
	top.Handle("/dashboard/", dashboardHandlers.StaticHandler())

	server := &http.Server{
		Addr:              *addr,
		Handler:           top,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(stdout, "throughline mcp listening on http://%s/mcp\n", *addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// loopbackHosts resolves addr's port and returns every loopback host:port form (and bare
// hostname) that daemonhttp should accept as Host/Origin, rejecting any non-loopback bind
// address outright: the daemon is never reachable from another host.
func loopbackHosts(addr string) ([]string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("parse --addr: %w", err)
	}
	switch host {
	case "127.0.0.1", "::1", "localhost", "":
	default:
		return nil, fmt.Errorf("--addr %q is not a loopback address; the daemon binds only 127.0.0.1", addr)
	}
	return []string{
		"127.0.0.1:" + port, "localhost:" + port, "[::1]:" + port,
		"127.0.0.1", "localhost", "[::1]",
	}, nil
}

// runDoctor is the single read-only diagnostic command: it checks the nearest workspace
// configuration, registry agreement, and daemon reachability, then points only to the
// authoritative setup/init remediation path. It never repairs or creates a second route.
func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", defaultAddr, "loopback address the daemon serves Streamable HTTP on")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("doctor takes no positional arguments")
	}

	directory, err := os.Getwd()
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "throughline doctor")

	workspace, workspaceErr := config.Find(directory)
	switch {
	case workspaceErr == nil:
		fmt.Fprintf(stdout, "workspace: found workspace_id=%s at %s\n", workspace.Config.WorkspaceID, workspace.Root)
	case errors.Is(workspaceErr, config.ErrLegacyWorkspace):
		fmt.Fprintf(stdout, "workspace: legacy_workspace_unsupported — %v\n", workspaceErr)
		fmt.Fprintln(stdout, "  remediation: export/archive this workspace, run `throughline setup`, then `throughline init`")
	default:
		fmt.Fprintf(stdout, "workspace: not found from %s\n", directory)
		fmt.Fprintln(stdout, "  remediation: run `throughline init` in the intended workspace root")
	}

	if workspaceErr == nil {
		registryHandle, err := openRegistry(ctx)
		if err != nil {
			return err
		}
		defer registryHandle.Close()
		target, lookupErr := registryHandle.Lookup(ctx, workspace.Config.WorkspaceID)
		switch {
		case lookupErr == nil:
			fmt.Fprintf(stdout, "registry: active, generation=%d\n", target.Generation)
			if fingerprintErr := registryHandle.CheckFingerprint(ctx, workspace.Config.WorkspaceID, workspace.Config.Fingerprint()); fingerprintErr != nil {
				fmt.Fprintf(stdout, "registry: %v\n", fingerprintErr)
				fmt.Fprintln(stdout, "  remediation: re-run `throughline init` from this workspace root")
			}
		default:
			fmt.Fprintf(stdout, "registry: %v\n", lookupErr)
			fmt.Fprintln(stdout, "  remediation: run `throughline init` to (re)register this workspace")
		}
	}

	health, healthErr := fetchHealth(ctx, *addr)
	if healthErr != nil {
		fmt.Fprintf(stdout, "daemon: unreachable at %s (%v)\n", *addr, healthErr)
		fmt.Fprintln(stdout, "  remediation: run `throughline mcp` (or `throughline setup` once available) to start the daemon")
		return nil
	}
	fmt.Fprintf(stdout, "daemon: reachable, version=%s\n", health.Version)
	return nil
}

// runDaemonStatus prints throughline daemon status --json's stable machine-readable
// contract. It currently reports only authenticated-health reachability; installed-service
// state (launchd/systemd) is a later work item's responsibility.
// daemonStateDir is the per-user directory the lock, PID, and log files live in, alongside
// the registry and credential. It follows registryPathForTesting automatically, so tests
// stay hermetic without a separate override.
func daemonStateDir() (string, error) {
	path, err := registryPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(path), nil
}

// executableForTesting overrides the binary daemon start/stop/restart spawns, since
// os.Executable() inside `go test` returns the test binary, not a real throughline build.
// Production has no alternate executable-selection mechanism; only tests set this.
var executableForTesting string

// newProcessManager builds the reference ServiceManager adapter this work item's own
// lifecycle commands use. launchd (WR-07) and systemd --user (WR-08) provide alternative
// ServiceManager implementations behind the same seam; which one throughline daemon uses is
// a later work item's concern, not this constructor's.
func newProcessManager(addr string) (*daemon.ProcessManager, error) {
	stateDir, err := daemonStateDir()
	if err != nil {
		return nil, err
	}
	executable := executableForTesting
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return nil, err
		}
	}
	return &daemon.ProcessManager{
		LockPath:   filepath.Join(stateDir, "daemon.lock"),
		PIDPath:    filepath.Join(stateDir, "daemon.pid"),
		LogPath:    filepath.Join(stateDir, "daemon.log"),
		Addr:       addr,
		Executable: executable,
		CheckHealth: func(ctx context.Context) (string, error) {
			health, err := fetchHealth(ctx, addr)
			return health.Version, err
		},
	}, nil
}

// serviceManagerForTesting overrides platform selection so daemon lifecycle tests can
// inject a fake without touching a real launchd/systemd instance. Production has no
// alternate selection mechanism; only tests set this.
var serviceManagerForTesting daemon.ServiceManager

// newServiceManager selects the one OS-managed service adapter throughline setup installs
// and throughline daemon's lifecycle subcommands control — the same instance either way, per
// the accepted decision that daemon status/start/stop/restart/logs always control that same
// service and never spawn an alternative server. Platforms without a native adapter here
// fall back to the reference ProcessManager.
func newServiceManager(addr string) (daemon.ServiceManager, error) {
	if serviceManagerForTesting != nil {
		return serviceManagerForTesting, nil
	}
	stateDir, err := daemonStateDir()
	if err != nil {
		return nil, err
	}
	executable := executableForTesting
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return nil, err
		}
	}
	logPath := filepath.Join(stateDir, "daemon.log")
	checkHealth := func(ctx context.Context) (string, error) {
		health, err := fetchHealth(ctx, addr)
		return health.Version, err
	}

	switch runtime.GOOS {
	case "darwin":
		plistPath, err := launchd.DefaultPlistPath()
		if err != nil {
			return nil, err
		}
		return &launchd.Manager{PlistPath: plistPath, Executable: executable, Addr: addr, LogPath: logPath, CheckHealth: checkHealth}, nil
	case "linux":
		unitPath, err := systemd.DefaultUnitPath()
		if err != nil {
			return nil, err
		}
		return &systemd.Manager{UnitPath: unitPath, Executable: executable, Addr: addr, LogPath: logPath, CheckHealth: checkHealth}, nil
	default:
		return newProcessManager(addr)
	}
}

// runDaemon dispatches throughline daemon's lifecycle subcommands. Every subcommand shares
// --addr so it targets the same endpoint runMCP would use with the same flag.
func runDaemon(ctx context.Context, subcommand string, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("daemon "+subcommand, flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", defaultAddr, "loopback address the daemon serves Streamable HTTP on")
	asJSON := flags.Bool("json", false, "print machine-readable JSON (status only)")
	lines := flags.Int("lines", 100, "number of trailing log lines to print (logs only)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("daemon %s takes no positional arguments", subcommand)
	}
	manager, err := newServiceManager(*addr)
	if err != nil {
		return err
	}

	switch subcommand {
	case "start":
		if err := manager.Start(ctx); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "started throughline daemon on http://%s/mcp\n", *addr)
		return nil
	case "stop":
		if err := manager.Stop(ctx); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "stopped throughline daemon")
		return nil
	case "restart":
		if err := manager.Restart(ctx); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "restarted throughline daemon on http://%s/mcp\n", *addr)
		return nil
	case "status":
		return printDaemonStatus(ctx, manager, *addr, *asJSON, stdout)
	case "logs":
		entries, err := manager.Logs(ctx, *lines)
		if err != nil {
			return err
		}
		for _, line := range entries {
			fmt.Fprintln(stdout, line)
		}
		return nil
	case "rotate-credential":
		return runRotateCredential(ctx, manager, *addr, stdout)
	default:
		return fmt.Errorf("unknown daemon subcommand %q", subcommand)
	}
}

func printDaemonStatus(ctx context.Context, manager daemon.ServiceManager, addr string, asJSON bool, stdout io.Writer) error {
	type statusOutput struct {
		Reachable bool   `json:"reachable"`
		Running   bool   `json:"running"`
		PID       int    `json:"pid,omitempty"`
		Version   string `json:"version,omitempty"`
		Error     string `json:"error,omitempty"`
	}
	result := statusOutput{}
	managerStatus, err := manager.Status(ctx)
	if err != nil {
		return err
	}
	result.Running = managerStatus.Running
	result.PID = managerStatus.PID
	health, err := fetchHealth(ctx, addr)
	if err != nil {
		result.Error = "unreachable"
	} else {
		result.Reachable = true
		result.Version = health.Version
	}

	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	fmt.Fprintf(stdout, "running=%v reachable=%v", result.Running, result.Reachable)
	if result.PID != 0 {
		fmt.Fprintf(stdout, " pid=%d", result.PID)
	}
	if result.Version != "" {
		fmt.Fprintf(stdout, " version=%s", result.Version)
	}
	fmt.Fprintln(stdout)
	return nil
}

// runRotateCredential regenerates the daemon's bearer token, restarts it, and verifies the
// new token actually authenticates before declaring success; daemon.RotateCredential rolls
// back and restarts again if either step fails.
func runRotateCredential(ctx context.Context, manager daemon.ServiceManager, addr string, stdout io.Writer) error {
	tokenPath, err := credentialPath()
	if err != nil {
		return err
	}
	_, err = daemon.RotateCredential(ctx, tokenPath, manager, func(ctx context.Context) error {
		_, healthErr := fetchHealth(ctx, addr)
		return healthErr
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "rotated daemon credential and verified the restarted daemon accepts it")
	return nil
}

// clientConfigPathsForTesting overrides the three managed harnesses' config/detect paths so
// setup/uninstall tests never touch a real ~/.codex, ~/.claude.json, or ~/.hermes. Production
// has no alternate selection mechanism; only tests set this.
var clientConfigPathsForTesting map[string][2]string // name -> [configPath, detectPath]

func clientTargets(addr string) ([]setup.Target, error) {
	type source struct {
		name        string
		defaultPath func() (string, error)
		detectDir   string // sibling directory whose existence detects installation
		reconcile   func(string, clientconfig.Entry, bool) (clientconfig.Result, error)
	}
	sources := []source{
		{"codex", codexconfig.DefaultPath, ".codex", codexconfig.Reconcile},
		{"claude-code", claudecodeconfig.DefaultPath, "", claudecodeconfig.Reconcile}, // detected by the config file itself
		{"hermes", hermesconfig.DefaultPath, ".hermes", hermesconfig.Reconcile},
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home directory: %w", err)
	}

	targets := make([]setup.Target, 0, len(sources))
	for _, s := range sources {
		if override, ok := clientConfigPathsForTesting[s.name]; ok {
			targets = append(targets, setup.Target{Name: s.name, ConfigPath: override[0], DetectPath: override[1], ReconcileFn: s.reconcile})
			continue
		}
		configPath, err := s.defaultPath()
		if err != nil {
			return nil, err
		}
		detectPath := configPath
		if s.detectDir != "" {
			detectPath = filepath.Join(home, s.detectDir)
		}
		targets = append(targets, setup.Target{Name: s.name, ConfigPath: configPath, DetectPath: detectPath, ReconcileFn: s.reconcile})
	}
	return targets, nil
}

// runSetup is throughline setup: the one-time, idempotent host setup that generates or
// loads the per-user credential, atomically reconciles every detected harness's global MCP
// configuration (backing each up first and rolling every one back together if any step
// fails), and installs and starts the one OS-managed daemon service. It never touches
// per-workspace state; throughline init remains the only way to initialize a workspace.
func runSetup(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", defaultAddr, "loopback address the managed daemon serves Streamable HTTP on")
	force := flags.Bool("force", false, "overwrite a conflicting existing entry in a managed harness configuration")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("setup takes no positional arguments")
	}

	tokenPath, err := credentialPath()
	if err != nil {
		return err
	}
	targets, err := clientTargets(*addr)
	if err != nil {
		return err
	}
	manager, err := newServiceManager(*addr)
	if err != nil {
		return err
	}

	result, err := setup.Run(ctx, setup.Options{
		CredentialPath: tokenPath,
		BearerEnvVar:   clientconfig.BearerEnvVar,
		DaemonURL:      "http://" + *addr + "/mcp",
		Targets:        targets,
		Manager:        manager,
		Force:          *force,
	})
	if err != nil {
		return err
	}

	if result.TokenCreated {
		fmt.Fprintln(stdout, "generated a new daemon credential")
	} else {
		fmt.Fprintln(stdout, "reused the existing daemon credential")
	}
	for _, target := range result.Targets {
		switch {
		case target.Skipped:
			fmt.Fprintf(stdout, "%s: not detected, skipped\n", target.Name)
		case target.Conflict:
			fmt.Fprintf(stdout, "%s: conflict, left untouched (%v); rerun with --force to overwrite\n", target.Name, target.Err)
		case target.Changed:
			fmt.Fprintf(stdout, "%s: configured\n", target.Name)
		default:
			fmt.Fprintf(stdout, "%s: already configured\n", target.Name)
		}
	}
	if result.ServiceStarted {
		fmt.Fprintf(stdout, "daemon started on http://%s/mcp\n", *addr)
	}
	return nil
}

// runUninstall stops and removes the managed daemon service and every managed client
// configuration entry this tool wrote, preserving all workspace data (every workspace's own
// database) and the registry unconditionally — uninstall never deletes coordination state,
// only routing/service configuration.
func runUninstall(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", defaultAddr, "loopback address the managed daemon serves Streamable HTTP on")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("uninstall takes no positional arguments")
	}

	manager, err := newServiceManager(*addr)
	if err != nil {
		return err
	}
	if err := manager.Stop(ctx); err != nil {
		return fmt.Errorf("stop daemon: %w", err)
	}
	fmt.Fprintln(stdout, "stopped the daemon service")

	targets, err := clientTargets(*addr)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if _, err := os.Stat(target.ConfigPath); os.IsNotExist(err) {
			continue
		}
		removed, err := removeClientEntry(target.ConfigPath, target.Name)
		if err != nil {
			fmt.Fprintf(stdout, "%s: %v\n", target.Name, err)
			continue
		}
		if removed {
			fmt.Fprintf(stdout, "%s: removed the throughline entry\n", target.Name)
		} else {
			fmt.Fprintf(stdout, "%s: no throughline entry to remove\n", target.Name)
		}
	}
	fmt.Fprintln(stdout, "workspace data and the registry were not touched")
	return nil
}

// removeClientEntry deletes the throughline key from one harness's configuration,
// preserving every other key, by delegating to that harness's own adapter package (which
// already owns the format-specific parsing Reconcile uses).
func removeClientEntry(path, name string) (bool, error) {
	switch name {
	case "codex":
		return codexconfig.Remove(path)
	case "claude-code":
		return claudecodeconfig.Remove(path)
	case "hermes":
		return hermesconfig.Remove(path)
	default:
		return false, fmt.Errorf("unknown managed client %q", name)
	}
}

// fetchHealth calls the daemon's authenticated /health endpoint using the locally stored
// credential. It never falls back to an unauthenticated request or another address.
func fetchHealth(ctx context.Context, addr string) (daemonhttp.HealthResponse, error) {
	tokenPath, err := credentialPath()
	if err != nil {
		return daemonhttp.HealthResponse{}, err
	}
	token, err := credential.LoadOrCreate(tokenPath)
	if err != nil {
		return daemonhttp.HealthResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health", nil)
	if err != nil {
		return daemonhttp.HealthResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return daemonhttp.HealthResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return daemonhttp.HealthResponse{}, fmt.Errorf("health returned status %d", response.StatusCode)
	}
	var health daemonhttp.HealthResponse
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		return daemonhttp.HealthResponse{}, err
	}
	return health, nil
}

// runReady and runShow are domain-facing commands: they discover the nearest workspace's
// identity from its config.toml and call the daemon's MCP endpoint for it, exactly as any
// other MCP client would. Neither opens a workspace database, a registry, or any other
// storage directly — domain-facing CLI commands are daemon clients, never provider
// instantiators, per the accepted decision.
func runReady(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("ready", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", defaultAddr, "loopback address the daemon serves Streamable HTTP on")
	actor := flags.String("actor", "", "actor_id to list ready work for (required; the list_ready_items tool requires a non-empty actor_id)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("expected at most one workspace directory")
	}
	if *actor == "" {
		return errors.New("--actor is required")
	}
	workspace, err := findWorkspace(optionalDirectory(flags.Args()))
	if err != nil {
		return err
	}
	result, err := callDaemonTool(ctx, *addr, "list_ready_items", map[string]any{"workspace_id": workspace.Config.WorkspaceID, "actor_id": *actor})
	if err != nil {
		return err
	}
	items, _ := result.([]any)
	for _, raw := range items {
		entry, _ := raw.(map[string]any)
		workItem, _ := entry["work_item"].(map[string]any)
		fmt.Fprintf(stdout, "%v\t%v\n", workItem["key"], workItem["title"])
	}
	return nil
}

func runShow(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("show", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", defaultAddr, "loopback address the daemon serves Streamable HTTP on")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 || flags.NArg() > 2 {
		return errors.New("expected work item id and optional workspace directory")
	}
	workspace, err := findWorkspace(optionalDirectory(flags.Args()[1:]))
	if err != nil {
		return err
	}
	result, err := callDaemonTool(ctx, *addr, "get_item", map[string]any{"workspace_id": workspace.Config.WorkspaceID, "id": flags.Arg(0)})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// findWorkspace resolves the nearest initialized workspace from directory (or the current
// directory if empty) by reading its config.toml alone; it never touches a database.
func findWorkspace(directory string) (config.Workspace, error) {
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return config.Workspace{}, fmt.Errorf("get current directory: %w", err)
		}
	}
	return config.Find(directory)
}

// authRoundTripper adds the daemon's bearer credential to every outgoing request, so
// domain-facing CLI commands authenticate exactly like any other MCP client.
type authRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (t authRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(cloned)
}

// callDaemonTool connects to the daemon at addr as one short-lived MCP client, calls one
// tool, and returns its "result" payload (or a descriptive error if the tool call itself
// failed). It never falls back to opening storage directly if the daemon is unreachable.
// DisableStandaloneSSE and an overall deadline (rather than http.Client.Timeout, which would
// also cap any long-lived streaming connection) keep a one-shot CLI call from hanging.
func callDaemonTool(ctx context.Context, addr, name string, arguments map[string]any) (any, error) {
	tokenPath, err := credentialPath()
	if err != nil {
		return nil, err
	}
	token, err := credential.LoadOrCreate(tokenPath)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client := protocol.NewClient(&protocol.Implementation{Name: "throughline-cli", Version: versionLine()}, nil)
	transport := &protocol.StreamableClientTransport{
		Endpoint:             "http://" + addr + "/mcp",
		HTTPClient:           &http.Client{Transport: authRoundTripper{token: token, base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to throughline daemon at %s: %w (is `throughline mcp` running?)", addr, err)
	}
	defer session.Close()

	response, err := session.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", name, err)
	}
	var payload map[string]any
	if len(response.Content) > 0 {
		if text, ok := response.Content[0].(*protocol.TextContent); ok {
			if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
				return nil, fmt.Errorf("decode %s response: %w", name, err)
			}
		}
	}
	if response.IsError {
		code, _ := payload["error"].(map[string]any)["code"].(string)
		message, _ := payload["error"].(map[string]any)["message"].(string)
		if code == "" {
			code = "error"
		}
		return nil, fmt.Errorf("%s: %s (%s)", name, message, code)
	}
	return payload["result"], nil
}

func optionalDirectory(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func runInit(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("database", "", "database path, absolute or relative to .throughline")
	fork := flags.Bool("fork", false, "create an independent workspace identity from a directory copied from another workspace")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("expected at most one workspace directory")
	}
	root := ""
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	} else {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get current directory: %w", err)
		}
	}

	registryHandle, err := openRegistry(ctx)
	if err != nil {
		return err
	}
	defer registryHandle.Close()

	if *fork {
		return runInitFork(ctx, registryHandle, root, stdout)
	}

	newID, err := app.UUIDv7Generator{}.New()
	if err != nil {
		return fmt.Errorf("generate workspace_id: %w", err)
	}
	workspace, created, err := config.Initialize(root, *databasePath, newID)
	if err != nil {
		return err
	}
	if err := registerWorkspace(ctx, registryHandle, workspace, ""); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(workspace.DatabasePath), 0o755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	database, err := throughlinesqlite.Open(ctx, workspace.DatabasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		return err
	}
	verb := "reopened"
	if created {
		verb = "initialized"
	}
	fmt.Fprintf(stdout, "%s Throughline workspace at %s\ndatabase: %s\nworkspace_id: %s\n", verb, workspace.Root, workspace.DatabasePath, workspace.Config.WorkspaceID)
	return nil
}

func runInitFork(ctx context.Context, registryHandle *registry.Registry, root string, stdout io.Writer) error {
	newID, err := app.UUIDv7Generator{}.New()
	if err != nil {
		return fmt.Errorf("generate workspace_id: %w", err)
	}
	workspace, sourceWorkspaceID, err := config.Fork(root, newID)
	if err != nil {
		return err
	}
	if err := registerWorkspace(ctx, registryHandle, workspace, sourceWorkspaceID); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "forked Throughline workspace at %s from workspace_id %s\ndatabase: %s\nworkspace_id: %s\n",
		workspace.Root, sourceWorkspaceID, workspace.DatabasePath, workspace.Config.WorkspaceID)
	return nil
}

// registerWorkspace performs one throughline init/fork registration attempt end to end:
// begin (fresh, reopen, move-reconciliation, or fork), then activate if it left pending.
// A canonical-root conflict with a still-present prior root (the copy-without-fork case)
// surfaces registry.ErrWorkspaceIdentityConflict unchanged, with remediation appended.
func registerWorkspace(ctx context.Context, registryHandle *registry.Registry, workspace config.Workspace, forkOf string) error {
	canonicalRoot, err := registry.CanonicalizeRoot(workspace.Root)
	if err != nil {
		return err
	}
	params := registry.BeginRegistrationParams{
		WorkspaceID:       workspace.Config.WorkspaceID,
		ProviderKind:      registry.ProviderSQLite,
		ProviderLocator:   workspace.Config.WorkspaceID,
		CanonicalRoot:     canonicalRoot,
		ConfigFingerprint: workspace.Config.Fingerprint(),
	}
	var result registry.RegistrationResult
	if forkOf != "" {
		result, err = registryHandle.Fork(ctx, forkOf, params)
	} else {
		result, err = registryHandle.BeginRegistration(ctx, params)
	}
	if err != nil {
		if errors.Is(err, registry.ErrWorkspaceIdentityConflict) {
			return fmt.Errorf("%w: %s is already registered at a different, still-present location; "+
				"use throughline init --fork to give this copy its own identity", err, workspace.Config.WorkspaceID)
		}
		return err
	}
	if result.Target.LifecycleState == registry.LifecycleActive {
		return nil
	}
	if _, err := registryHandle.Activate(ctx, workspace.Config.WorkspaceID, result.Target.Generation); err != nil {
		return err
	}
	return nil
}

func runUnregister(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("unregister", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("expected at most one workspace directory")
	}
	root := ""
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	} else {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get current directory: %w", err)
		}
	}
	workspace, err := config.Load(root)
	if err != nil {
		return err
	}
	registryHandle, err := openRegistry(ctx)
	if err != nil {
		return err
	}
	defer registryHandle.Close()
	target, err := registryHandle.Lookup(ctx, workspace.Config.WorkspaceID)
	if err != nil {
		return err
	}
	if err := registryHandle.Unregister(ctx, workspace.Config.WorkspaceID, target.Generation); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "unregistered workspace_id %s (%s); workspace data at %s is untouched\n",
		workspace.Config.WorkspaceID, workspace.Root, workspace.DatabasePath)
	return nil
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: throughline <init|unregister|setup|uninstall|ready|show|mcp|doctor|daemon <start|stop|restart|status|logs|rotate-credential>|version> [arguments]")
}
