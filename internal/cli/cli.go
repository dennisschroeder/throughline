package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dennisschroeder/workgraph/internal/app"
	"github.com/dennisschroeder/workgraph/internal/config"
	workgraphmcp "github.com/dennisschroeder/workgraph/internal/mcp"
	workgraphsqlite "github.com/dennisschroeder/workgraph/internal/sqlite"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeUsage(stderr)
		return 2
	}
	switch args[0] {
	case "init":
		if err := runInit(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "workgraph init: %v\n", err)
			return 1
		}
		return 0
	case "ready":
		if err := runReady(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "workgraph ready: %v\n", err)
			return 1
		}
		return 0
	case "show":
		if err := runShow(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "workgraph show: %v\n", err)
			return 1
		}
		return 0
	case "mcp":
		if err := runMCP(ctx, args[1:], stderr); err != nil {
			fmt.Fprintf(stderr, "workgraph mcp: %v\n", err)
			return 1
		}
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

func runMCP(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("expected at most one workspace directory")
	}
	service, closeDatabase, err := workspaceService(ctx, optionalDirectory(flags.Args()))
	if err != nil {
		return err
	}
	defer closeDatabase()
	return workgraphmcp.Run(ctx, service)
}

func runReady(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("ready", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("expected at most one workspace directory")
	}
	service, closeDatabase, err := workspaceService(ctx, optionalDirectory(flags.Args()))
	if err != nil {
		return err
	}
	defer closeDatabase()
	items, err := service.ListReadyWork(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		fmt.Fprintf(stdout, "%s\t%s\n", item.WorkItem.Key, item.WorkItem.Title)
	}
	return nil
}

func runShow(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("show", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 || flags.NArg() > 2 {
		return errors.New("expected work item id and optional workspace directory")
	}
	service, closeDatabase, err := workspaceService(ctx, optionalDirectory(flags.Args()[1:]))
	if err != nil {
		return err
	}
	defer closeDatabase()
	item, err := service.GetWorkItem(ctx, flags.Arg(0))
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(item)
}

func workspaceService(ctx context.Context, directory string) (*app.Service, func(), error) {
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return nil, nil, fmt.Errorf("get current directory: %w", err)
		}
	}
	workspace, err := config.Find(directory)
	if err != nil {
		return nil, nil, err
	}
	database, err := workgraphsqlite.Open(ctx, workspace.DatabasePath)
	if err != nil {
		return nil, nil, err
	}
	if err := database.Migrate(ctx); err != nil {
		_ = database.Close()
		return nil, nil, err
	}
	return app.NewService(database.Store(), app.UUIDv7Generator{}, app.SystemClock{}), func() { _ = database.Close() }, nil
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
	databasePath := flags.String("database", "", "database path, absolute or relative to .workgraph")
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
	workspace, created, err := config.Initialize(root, *databasePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(workspace.DatabasePath), 0o755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	database, err := workgraphsqlite.Open(ctx, workspace.DatabasePath)
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
	fmt.Fprintf(stdout, "%s Workgraph workspace at %s\ndatabase: %s\n", verb, workspace.Root, workspace.DatabasePath)
	return nil
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: workgraph <init|ready|show|mcp> [arguments]")
}
