package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hermes-at/cbl/internal/cbl"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return printHelp()
	}

	switch args[0] {
	case "status":
		return runStatus(ctx, args[1:])
	case "serve":
		return runServe(ctx, args[1:])
	case "watch":
		return runWatch(ctx, args[1:])
	case "version", "--version", "-v":
		fmt.Println("cbl dev")
		return nil
	case "help", "--help", "-h":
		return printHelp()
	default:
		if args[0] == "-json" {
			return runStatus(ctx, args)
		}
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var opts cbl.Options
	fs.StringVar(&opts.AuthFile, "auth", "", "path to Codex auth.json")
	fs.StringVar(&opts.ConfigFile, "config", "", "path to config.toml")
	fs.StringVar(&opts.BaseURL, "base-url", "", "override ChatGPT base URL")
	fs.StringVar(&opts.Fixture, "fixture", "", "read usage JSON from a file instead of calling the API")
	fs.BoolVar(&opts.JSON, "json", false, "print machine-readable JSON")
	fs.BoolVar(&opts.Waybar, "waybar", false, "print Waybar JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	snap, err := cbl.Load(ctx, opts)
	if err != nil {
		return err
	}
	return cbl.Render(os.Stdout, snap, cbl.RenderOptions{JSON: opts.JSON, Waybar: opts.Waybar})
}

func runWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var opts cbl.Options
	interval := fs.Duration("interval", 5*time.Minute, "refresh interval")
	fs.StringVar(&opts.AuthFile, "auth", "", "path to Codex auth.json")
	fs.StringVar(&opts.ConfigFile, "config", "", "path to config.toml")
	fs.StringVar(&opts.BaseURL, "base-url", "", "override ChatGPT base URL")
	fs.StringVar(&opts.Fixture, "fixture", "", "read usage JSON from a file instead of calling the API")
	fs.BoolVar(&opts.Waybar, "waybar", false, "print Waybar JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *interval <= 0 {
		return fmt.Errorf("interval must be > 0")
	}
	return cbl.Watch(ctx, *interval, opts, os.Stdout)
}

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var opts cbl.Options
	addr := fs.String("addr", "127.0.0.1:8088", "listen address")
	interval := fs.Duration("interval", 5*time.Minute, "refresh interval")
	fs.StringVar(&opts.AuthFile, "auth", "", "path to Codex auth.json")
	fs.StringVar(&opts.ConfigFile, "config", "", "path to config.toml")
	fs.StringVar(&opts.BaseURL, "base-url", "", "override ChatGPT base URL")
	fs.StringVar(&opts.Fixture, "fixture", "", "read usage JSON from a file instead of calling the API")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *interval <= 0 {
		return fmt.Errorf("interval must be > 0")
	}
	return cbl.Serve(ctx, *addr, *interval, opts)
}

func printHelp() error {
	fmt.Print(`cbl - Codex bar for Linux

Usage:
  cbl status [--json|--waybar] [--auth PATH] [--config PATH] [--base-url URL]
  cbl watch  [--interval 5m] [--waybar]
  cbl serve  [--addr 127.0.0.1:8088] [--interval 5m]

Environment:
  CBL_AUTH_FILE   path to auth.json
  CBL_CONFIG_FILE  path to config.toml
  CBL_BASE_URL     override ChatGPT base URL
  CBL_FIXTURE      response JSON fixture for offline use

Default API path:
  https://chatgpt.com/backend-api/wham/usage
`)
	return nil
}
