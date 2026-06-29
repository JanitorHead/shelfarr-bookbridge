package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources/goodreads"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdout)) }

func run(args []string, getenv func(string) string, out io.Writer) int {
	if len(args) == 0 || args[0] != "sync" {
		fmt.Fprintln(out, "usage: bookbridge sync [--dry-run|--apply] [--baseline]")
		return 2
	}
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(out)
	apply := fs.Bool("apply", false, "create requests (default is dry-run)")
	dry := fs.Bool("dry-run", false, "preview only")
	baseline := fs.Bool("baseline", false, "mark current shelf contents as seen, request nothing")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	dryRun := !*apply || *dry

	cfg, err := config.Load2(getenv)
	if err != nil {
		fmt.Fprintln(out, "config error:", err)
		return 1
	}
	st, err := store.Open(orEnv(getenv, "BB_DB", "/config/bookbridge.db"))
	if err != nil {
		fmt.Fprintln(out, "store error:", err)
		return 1
	}
	defer st.Close()

	src := goodreads.NewRSSSource(cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, getenv("GOODREADS_BASE"), nil)
	sh := shelfarr.New(cfg.ShelfarrURL, cfg.ShelfarrToken, nil)
	ctx := context.Background()

	if *baseline {
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

	e := engine.New(src, st, sh, cfg)
	rep, err := e.Run(ctx, dryRun)
	if err != nil {
		fmt.Fprintln(out, "run error:", err)
		return 1
	}
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Fprintf(out, "[%s] fetched=%d new=%d requested=%d not_found=%d already_exists=%d\n",
		mode, rep.Fetched, rep.New, rep.Requested, rep.NotFound, rep.AlreadyExists)
	return 0
}

func orEnv(get func(string) string, k, def string) string {
	if v := get(k); v != "" {
		return v
	}
	return def
}
