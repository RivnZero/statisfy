// Command statisfy shows the real status of AI coding tools available on this
// machine: plans, quotas, rate limits, session limits, tokens, and cost.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"statisfy/internal/adapters"
	"statisfy/internal/cache"
	"statisfy/internal/config"
	"statisfy/internal/core"
	"statisfy/internal/render"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	args = reorderArgs(args)
	fs := flag.NewFlagSet("statisfy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "output normalized JSON (no ANSI)")
	refresh := fs.Bool("refresh", false, "ignore cache and fetch fresh data")
	all := fs.Bool("all", false, "include unavailable integrations")
	showVer := fs.Bool("version", false, "print version and exit")
	watchFlag := fs.Bool("watch", false, "live refreshing dashboard")
	cfgPath := fs.String("config", configPathFromEnv(), "path to config file (default: platform config dir)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showVer {
		printVersion(stdout)
		return 0
	}

	rest := fs.Args()
	if len(rest) > 0 {
		switch rest[0] {
		case "doctor":
			return runDoctor(stdout, stderr, *refresh, *cfgPath)
		case "sources":
			return runSources(stdout, stderr, *refresh, *cfgPath)
		case "watch":
			*watchFlag = true
			rest = rest[1:] // consume the positional so it's not treated as a tool
		case "version":
			printVersion(stdout)
			return 0
		case "help", "--help", "-h":
			printUsage(stderr)
			return 0
		}
	}

	var tool string
	if len(rest) > 0 {
		tool = rest[0]
	}

	cfg := loadConfigOrWarn(*cfgPath, stderr)

	// Apply config: drop disabled adapters.
	allAdapters := adapters.Defaults()
	enabled := make([]core.Adapter, 0, len(allAdapters))
	for _, a := range allAdapters {
		if cfg.AdapterEnabled(a.ID()) {
			enabled = append(enabled, a)
		}
	}
	reg := core.NewRegistry(enabled...)

	if tool != "" {
		a, ok := reg.Get(tool)
		if !ok {
			fmt.Fprintf(stderr, "unknown or disabled tool %q (supported: %s)\n", tool, strings.Join(reg.IDs(), ", "))
			return 2
		}
		// A single-tool command only detects and fetches that tool. This keeps
		// `statisfy freebuff` from spawning unrelated adapters (e.g. the codex
		// app-server) and minimizes the surface statisfy touches at all.
		reg = core.NewRegistry(a)
	}

	ctx := context.Background()
	timeout := cfg.Timeout

	if *watchFlag {
		return runWatch(ctx, reg, os.Stdin, stdout, cfg, newCache(cfg), *refresh)
	}

	detections := core.DetectAll(ctx, reg)
	c := newCache(cfg)
	opts := core.Options{Refresh: *refresh, All: *all, PerFetchTimeout: timeout, Cache: c}

	var results []core.ToolResult
	if tool != "" {
		results = core.Collect(ctx, reg, detections, opts)
	} else {
		results = core.Collect(ctx, reg, detections, opts)
		if !*all {
			results = core.FilterAvailable(results)
		}
	}

	if *jsonOut {
		doc := render.JSONDocFromResults(results, *all)
		if err := render.WriteJSON(stdout, doc); err != nil {
			fmt.Fprintf(stderr, "json output: %v\n", err)
			return 1
		}
		return 0
	}

	ropts := render.Options{Color: render.DetectColor(), ShowErrors: *all || tool != ""}

	if len(results) == 0 {
		fmt.Fprintln(stdout, "No supported AI coding tools were detected.")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Run:")
		fmt.Fprintln(stdout, "  statisfy doctor")
		return 0
	}

	render.Dashboard(stdout, results, ropts)
	return 0
}

func runDoctor(stdout, stderr io.Writer, refresh bool, cfgPath string) int {
	cfg := loadConfigOrWarn(cfgPath, stderr)
	ctx := context.Background()
	reg := core.NewRegistry(adapters.Defaults()...)
	detections := core.DetectAll(ctx, reg)
	opts := core.Options{Refresh: refresh, PerFetchTimeout: cfg.Timeout, Cache: newCache(cfg)}
	results := core.Collect(ctx, reg, detections, opts)

	if cfgPath != "" {
		fmt.Fprintf(stdout, "config: %s\n", cfgPath)
		fmt.Fprintf(stdout, "cache_ttl: %s   timeout: %s\n", cfg.CacheTTL, cfg.Timeout)
		if len(cfg.Disabled) > 0 {
			ids := make([]string, 0, len(cfg.Disabled))
			for id := range cfg.Disabled {
				ids = append(ids, id)
			}
			fmt.Fprintf(stdout, "disabled: %s\n", strings.Join(ids, ", "))
		}
		fmt.Fprintln(stdout)
	}
	render.Doctor(stdout, results, render.Options{Color: render.DetectColor(), ShowErrors: true})
	return 0
}

// runSources renders provenance for every detected integration.
func runSources(stdout, stderr io.Writer, refresh bool, cfgPath string) int {
	cfg := loadConfigOrWarn(cfgPath, stderr)
	ctx := context.Background()
	reg := core.NewRegistry(adapters.Defaults()...)
	detections := core.DetectAll(ctx, reg)
	opts := core.Options{Refresh: refresh, PerFetchTimeout: cfg.Timeout, Cache: newCache(cfg)}
	results := core.Collect(ctx, reg, detections, opts)
	render.Sources(stdout, results, render.Options{Color: render.DetectColor()})
	return 0
}

func newCache(cfg *config.Config) *cache.Cache {
	// Global default TTL plus per-adapter overrides.
	opts := []cache.Option{cache.WithTTL(cfg.CacheTTL)}
	for id, ttl := range cfg.AdapterTTL {
		opts = append(opts, cache.WithOverride(id, ttl))
	}
	return cache.New(cache.DefaultDir(), cfg.CacheTTL, opts...)
}

// loadConfigOrWarn loads the user config. A missing file is silent (config is
// optional). A file that exists but is invalid prints an actionable diagnostic
// to stderr and falls back to built-in defaults — invalid values are never
// silently accepted or silently reinterpreted.
func loadConfigOrWarn(path string, stderr io.Writer) *config.Config {
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "statisfy: %v; using built-in defaults (fix or remove the config file)\n", err)
		return config.Default()
	}
	return cfg
}

// reorderArgs moves flags (and their values) ahead of positional arguments so
// that `statisfy watch --refresh` parses identically to `statisfy --refresh
// watch`. Only --config takes a separate value in the current flag set.
func reorderArgs(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--config" {
			flags = append(flags, a)
			if i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}

func configPathFromEnv() string {
	if p := os.Getenv("STATISFY_CONFIG"); p != "" {
		return p
	}
	return config.ConfigPath()
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "statisfy %s (commit %s, built %s)\n", version, commit, date)
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "statisfy — view AI coding tool usage, quotas, and plans")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  statisfy                    show available integrations")
	fmt.Fprintln(w, "  statisfy <tool>             show one tool (codex|claude|opencode|freebuff|gemini|copilot|qwen|openrouter|cline|aider|droid)")
	fmt.Fprintln(w, "  statisfy doctor             diagnose installation/configuration problems")
	fmt.Fprintln(w, "  statisfy sources            show where each value comes from (provenance)")
	fmt.Fprintln(w, "  statisfy watch              live refreshing dashboard")
	fmt.Fprintln(w, "  statisfy version            print version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --json      machine-readable normalized JSON output")
	fmt.Fprintln(w, "  --refresh   bypass the cache and fetch fresh data")
	fmt.Fprintln(w, "  --all       include unavailable integrations")
	fmt.Fprintln(w, "  --config    path to config file (default: platform config dir)")
	fmt.Fprintln(w, "  --version   print version and exit")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Config (optional): ~/.config/statisfy/config.toml (platform-aware)")
}
