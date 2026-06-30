package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/auth"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/langdetect"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/scheduler"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources/goodreads"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/web"
)

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdout)) }

func run(args []string, getenv func(string) string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(out, "usage: bookbridge <sync|daemon|web> [flags]")
		return 2
	}
	switch args[0] {
	case "sync":
		return runSync(args[1:], getenv, out)
	case "daemon":
		return runDaemon(args[1:], getenv, out)
	case "web":
		return runWeb(args[1:], getenv, out)
	default:
		fmt.Fprintln(out, "usage: bookbridge <sync|daemon|web> [flags]")
		return 2
	}
}

// effectiveConfig merges env with stored settings.
func effectiveConfig(st *store.Store, getenv func(string) string) (config.Config, error) {
	all, err := st.AllSettings(context.Background())
	if err != nil {
		return config.Config{}, err
	}
	return config.LoadEffective(getenv, all)
}

// bootstrapAdmin seeds AUTH_USERNAME/AUTH_PASSWORD_HASH from env on first start.
func bootstrapAdmin(st *store.Store, getenv func(string) string) {
	ctx := context.Background()
	if _, ok, _ := st.GetSetting(ctx, "AUTH_PASSWORD_HASH"); ok {
		return
	}
	if pw := getenv("AUTH_PASSWORD"); pw != "" {
		u := getenv("AUTH_USERNAME")
		if u == "" {
			u = "admin"
		}
		if h, err := auth.Hash(pw); err == nil {
			st.SetSetting(ctx, "AUTH_USERNAME", u)
			st.SetSetting(ctx, "AUTH_PASSWORD_HASH", h)
		}
	}
}

// engineFor builds an engine from effective config against an EXISTING store.
func engineFor(cfg config.Config, st *store.Store, getenv func(string) string) (*engine.Engine, error) {
	if err := config.CheckTransport(cfg.ShelfarrURL, cfg.ShelfarrInsecure); err != nil {
		return nil, err
	}
	src := goodreads.NewSource(cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, cfg.GoodreadsCookie, getenv("GOODREADS_BASE"), nil)
	sh := shelfarr.New(cfg.ShelfarrURL, cfg.ShelfarrToken, &http.Client{Timeout: 20 * time.Second})
	e := engine.New(src, st, sh, cfg)
	if cfg.LangInference {
		e.SetDetector(langdetect.New())
	}
	return e, nil
}

func printReport(out io.Writer, mode string, rep engine.Report) {
	fmt.Fprintf(out, "[%s] fetched=%d new=%d requested=%d not_found=%d already_exists=%d errors=%d reconciled=%d completed=%d failed=%d rechecked=%d parked=%d\n",
		mode, rep.Fetched, rep.New, rep.Requested, rep.NotFound, rep.AlreadyExists, rep.Errors,
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

	st, err := store.Open(orEnv(getenv, "BB_DB", "/config/bookbridge.db"))
	if err != nil {
		fmt.Fprintln(out, "store error:", err)
		return 1
	}
	defer st.Close()
	bootstrapAdmin(st, getenv)
	cfg, err := effectiveConfig(st, getenv)
	if err != nil {
		fmt.Fprintln(out, "config error:", err)
		return 1
	}
	ctx := context.Background()

	if *baseline {
		if err := config.CheckTransport(cfg.ShelfarrURL, cfg.ShelfarrInsecure); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
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

	e, err := engineFor(cfg, st, getenv)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
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
	st, err := store.Open(orEnv(getenv, "BB_DB", "/config/bookbridge.db"))
	if err != nil {
		fmt.Fprintln(out, "store error:", err)
		return 1
	}
	defer st.Close()
	bootstrapAdmin(st, getenv)
	cfg, err := effectiveConfig(st, getenv)
	if err != nil {
		fmt.Fprintln(out, "config error:", err)
		return 1
	}
	e, err := engineFor(cfg, st, getenv)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}

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

	// serve the GUI alongside the scheduler so one container serves both
	go func() {
		runner := func(dryRun bool) (engine.Report, error) {
			rcfg, err := effectiveConfig(st, getenv)
			if err != nil {
				return engine.Report{}, err
			}
			e2, err := engineFor(rcfg, st, getenv)
			if err != nil {
				return engine.Report{}, err
			}
			return e2.Run(context.Background(), dryRun)
		}
		srv := web.New(st, runner)
		addr := net.JoinHostPort(cfg.GUIBind, cfg.GUIPort)
		fmt.Fprintf(out, "BookBridge GUI on http://%s\n", addr)
		http.ListenAndServe(addr, srv.Handler())
	}()

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

func runWeb(args []string, getenv func(string) string, out io.Writer) int {
	st, err := store.Open(orEnv(getenv, "BB_DB", "/config/bookbridge.db"))
	if err != nil {
		fmt.Fprintln(out, "store error:", err)
		return 1
	}
	defer st.Close()
	bootstrapAdmin(st, getenv)
	runner := func(dryRun bool) (engine.Report, error) {
		cfg, err := effectiveConfig(st, getenv)
		if err != nil {
			return engine.Report{}, err
		}
		e, err := engineFor(cfg, st, getenv)
		if err != nil {
			return engine.Report{}, err
		}
		return e.Run(context.Background(), dryRun)
	}
	srv := web.New(st, runner)
	cfg, _ := effectiveConfig(st, getenv)
	addr := net.JoinHostPort(cfg.GUIBind, cfg.GUIPort)
	fmt.Fprintf(out, "BookBridge GUI on http://%s\n", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		fmt.Fprintln(out, "web error:", err)
		return 1
	}
	return 0
}

func orEnv(get func(string) string, k, def string) string {
	if v := get(k); v != "" {
		return v
	}
	return def
}
