package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/langdetect"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/scheduler"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources/goodreads"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdout)) }

func run(args []string, getenv func(string) string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(out, "usage: bookbridge <sync|daemon> [flags]")
		return 2
	}
	switch args[0] {
	case "sync":
		return runSync(args[1:], getenv, out)
	case "daemon":
		return runDaemon(args[1:], getenv, out)
	default:
		fmt.Fprintln(out, "usage: bookbridge <sync|daemon> [flags]")
		return 2
	}
}

func buildEngine(cfg config.Config, getenv func(string) string) (*engine.Engine, *store.Store, error) {
	if err := config.CheckTransport(cfg.ShelfarrURL, cfg.ShelfarrInsecure); err != nil {
		return nil, nil, err
	}
	st, err := store.Open(orEnv(getenv, "BB_DB", "/config/bookbridge.db"))
	if err != nil {
		return nil, nil, err
	}
	src := goodreads.NewSource(cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, cfg.GoodreadsCookie, getenv("GOODREADS_BASE"), nil)
	sh := shelfarr.New(cfg.ShelfarrURL, cfg.ShelfarrToken, nil)
	e := engine.New(src, st, sh, cfg)
	if cfg.LangInference {
		e.SetDetector(langdetect.New())
	}
	return e, st, nil
}

func printReport(out io.Writer, mode string, rep engine.Report) {
	fmt.Fprintf(out, "[%s] fetched=%d new=%d requested=%d not_found=%d already_exists=%d reconciled=%d completed=%d failed=%d rechecked=%d parked=%d\n",
		mode, rep.Fetched, rep.New, rep.Requested, rep.NotFound, rep.AlreadyExists,
		rep.Reconciled, rep.Completed, rep.Failed, rep.Rechecked, rep.Parked)
}

func runSync(args []string, getenv func(string) string, out io.Writer) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(out)
	apply := fs.Bool("apply", false, "create requests (default is dry-run)")
	dry := fs.Bool("dry-run", false, "preview only")
	baseline := fs.Bool("baseline", false, "mark current shelf contents as seen, request nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dryRun := !*apply || *dry

	cfg, err := config.Load2(getenv)
	if err != nil {
		fmt.Fprintln(out, "config error:", err)
		return 1
	}
	ctx := context.Background()

	if *baseline {
		e, st, err := buildEngine(cfg, getenv)
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		defer st.Close()
		_ = e
		src := goodreads.NewSource(cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, cfg.GoodreadsCookie, getenv("GOODREADS_BASE"), nil)
		books, err := src.Fetch(ctx, cfg.Shelves)
		if err != nil {
			fmt.Fprintln(out, "fetch error:", err)
			return 1
		}
		if _, err := st.Diff(ctx, books); err != nil {
			fmt.Fprintln(out, "diff error:", err)
			return 1
		}
		for _, shelf := range cfg.Shelves {
			if err := st.BaselineShelf(ctx, shelf); err != nil {
				fmt.Fprintln(out, "baseline error:", err)
				return 1
			}
		}
		fmt.Fprintln(out, "baseline complete")
		return 0
	}

	e, st, err := buildEngine(cfg, getenv)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	defer st.Close()
	rep, err := e.Run(ctx, dryRun)
	if err != nil {
		fmt.Fprintln(out, "run error:", err)
		return 1
	}
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	printReport(out, mode, rep)
	return 0
}

func runDaemon(args []string, getenv func(string) string, out io.Writer) int {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(out)
	once := fs.Bool("once", false, "run a single cycle and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load2(getenv)
	if err != nil {
		fmt.Fprintln(out, "config error:", err)
		return 1
	}
	e, st, err := buildEngine(cfg, getenv)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	defer st.Close()

	cycle := func() {
		rep, err := e.Run(context.Background(), false)
		if err != nil {
			fmt.Fprintln(out, "run error:", err)
			return
		}
		printReport(out, "daemon", rep)
	}

	cycle() // run once immediately
	if *once {
		return 0
	}
	sch, err := scheduler.New(cfg.Schedule, cycle)
	if err != nil {
		fmt.Fprintln(out, "schedule error:", err)
		return 1
	}
	sch.Start()
	defer sch.Stop()
	fmt.Fprintf(out, "daemon scheduled on %q; waiting...\n", cfg.Schedule)
	select {} // block forever
}

func orEnv(get func(string) string, k, def string) string {
	if v := get(k); v != "" {
		return v
	}
	return def
}
