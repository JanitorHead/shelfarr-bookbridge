package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/auth"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/cwa"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/langdetect"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/resolver"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/scheduler"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources/goodreads"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources/hardcover"
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
	src := sourceFor(cfg, getenv)
	sh := shelfarr.New(cfg.ShelfarrURL, cfg.ShelfarrToken, &http.Client{Timeout: 20 * time.Second})
	e := engine.New(src, st, sh, cfg)
	if cfg.LangInference {
		e.SetDetector(langdetect.New())
	}
	return e, nil
}

// newWebServer builds the GUI server wired with a runner and a shelf discoverer
// (the discoverer is rebuilt from effective config per call so it always uses the
// latest cookie/mode the user saved in Settings).
func newWebServer(st *store.Store, getenv func(string) string) *web.Server {
	srv := web.New(st, func(dryRun bool) (engine.Report, error) { return runOnce(st, getenv, dryRun) })
	srv.SetDiscoverer(func(ctx context.Context) ([]sources.Shelf, error) {
		cfg, err := effectiveConfig(st, getenv)
		if err != nil {
			return nil, err
		}
		src := sourceFor(cfg, getenv)
		lister, ok := src.(sources.ShelfLister)
		if !ok {
			return nil, fmt.Errorf("listing shelves needs the Goodreads session cookie — set \"Goodreads source mode\" to private and paste your cookie in Settings")
		}
		return lister.ListShelves(ctx)
	})
	return srv
}

// sourceFor builds the ingest source from effective config (explicit selection by
// cfg.Source; Goodreads sub-mode by cfg.GoodreadsMode).
func sourceFor(cfg config.Config, getenv func(string) string) sources.Source {
	if cfg.Source == "hardcover" {
		return hardcover.NewSource(cfg.HardcoverToken, getenv("HARDCOVER_BASE"), nil)
	}
	return goodreads.NewSource(cfg.GoodreadsMode, cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, cfg.GoodreadsCookie, getenv("GOODREADS_BASE"), nil)
}

// runOnce builds the engine from effective config and runs one cycle. It returns
// a clear error (without crashing the process) when Shelfarr is not configured
// yet, so the daemon/GUI keeps running until you set it up in Settings.
func runOnce(st *store.Store, getenv func(string) string, dryRun bool) (engine.Report, error) {
	cfg, err := effectiveConfig(st, getenv)
	if err != nil {
		return engine.Report{}, err
	}
	if !cfg.ShelfarrConfigured() {
		return engine.Report{}, fmt.Errorf("Shelfarr is not configured yet — open the GUI and set the Shelfarr URL + token in Settings")
	}
	e, err := engineFor(cfg, st, getenv)
	if err != nil {
		return engine.Report{}, err
	}
	start := time.Now()
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	rep, runErr := e.Run(context.Background(), dryRun)
	rec := store.RunRecord{
		StartedAt: start, FinishedAt: time.Now(), Mode: mode, OK: runErr == nil,
		Fetched: rep.Fetched, New: rep.New, Requested: rep.Requested, NotFound: rep.NotFound,
		Errors: rep.Errors, Summary: reportLine(mode, rep),
	}
	if runErr != nil {
		rec.ErrorText = runErr.Error()
	}
	_, _ = st.RecordRun(context.Background(), rec)
	// After a successful real run, push Goodreads shelves into CWA as tags.
	if runErr == nil && !dryRun && cfg.CWAConfigured() {
		cwaTagPass(st, cfg, os.Stdout)
	}
	return rep, runErr
}

// cwaTagPass tags downloaded ('done') books in the Calibre library (via CWA) with
// their Goodreads shelves as "gr:<shelf>" tags, merged with the book's existing
// tags. Each book is tagged at most once (cwa_tagged flag).
func cwaTagPass(st *store.Store, cfg config.Config, out io.Writer) {
	ctx := context.Background()
	pending, err := st.DoneUntaggedForCWA(ctx)
	if err != nil || len(pending) == 0 {
		return
	}
	client := cwa.New(cfg.CWAURL, cfg.CWAUsername, cfg.CWAPassword)
	if err := client.Login(ctx); err != nil {
		fmt.Fprintln(out, "[cwa]", err)
		return
	}
	lib, err := client.ListBooks(ctx)
	if err != nil {
		fmt.Fprintln(out, "[cwa]", err)
		return
	}
	tagged := 0
	for _, b := range pending {
		cal := bestCalibreMatch(b, lib)
		if cal == nil {
			continue // not in the Calibre library yet
		}
		if err := client.SetTags(ctx, cal.ID, mergeGRTags(cal.Tags, b.Shelves)); err != nil {
			fmt.Fprintln(out, "[cwa]", err)
			continue
		}
		_ = st.MarkCWATagged(ctx, b.Source, b.ExternalID)
		tagged++
	}
	if tagged > 0 {
		fmt.Fprintf(out, "[cwa] tagged %d book(s) in Calibre with their Goodreads shelves\n", tagged)
	}
}

// bestCalibreMatch finds the Calibre book matching a tracked book by title+author.
func bestCalibreMatch(b store.BookRow, lib []cwa.Book) *cwa.Book {
	var pick *cwa.Book
	bestScore := -1.0
	for i := range lib {
		c := &lib[i]
		score := 0.7*resolver.TitleSimilarity(b.Title, c.Title) + 0.3*resolver.Similarity(flipAuthor(b.Author), c.Authors)
		if score > bestScore {
			bestScore = score
			pick = c
		}
	}
	if bestScore >= 0.6 {
		return pick
	}
	return nil
}

// flipAuthor turns "Last, First" into "First Last" to match Calibre's author form.
func flipAuthor(a string) string {
	if i := strings.Index(a, ","); i >= 0 {
		return strings.TrimSpace(a[i+1:]) + " " + strings.TrimSpace(a[:i])
	}
	return a
}

// mergeGRTags unions a book's existing CWA tags with "gr:<shelf>" tags.
func mergeGRTags(existing, shelves []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, t := range existing {
		add(t)
	}
	for _, sh := range shelves {
		add("gr:" + sh)
	}
	return out
}

func reportLine(mode string, rep engine.Report) string {
	return fmt.Sprintf("[%s] fetched=%d new=%d requested=%d not_found=%d already_exists=%d errors=%d reconciled=%d completed=%d failed=%d rechecked=%d parked=%d",
		mode, rep.Fetched, rep.New, rep.Requested, rep.NotFound, rep.AlreadyExists, rep.Errors,
		rep.Reconciled, rep.Completed, rep.Failed, rep.Rechecked, rep.Parked)
}

func printReport(out io.Writer, mode string, rep engine.Report) {
	fmt.Fprintln(out, reportLine(mode, rep))
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
		// baseline only reads Goodreads + marks the store; it does not touch Shelfarr.
		src := sourceFor(cfg, getenv)
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

	rep, err := runOnce(st, getenv, dryRun)
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

	cycle := func() {
		rep, err := runOnce(st, getenv, false)
		if err != nil {
			fmt.Fprintln(out, "[daemon]", err)
			return
		}
		printReport(out, "daemon", rep)
	}

	cycle() // run once immediately (skips gracefully if Shelfarr isn't configured)
	if *once {
		return 0
	}

	// Serve the GUI alongside the scheduler — ALWAYS, even before Shelfarr is
	// configured, so it can be set up in the GUI.
	go func() {
		srv := newWebServer(st, getenv)
		addr := net.JoinHostPort(cfg.GUIBind, cfg.GUIPort)
		fmt.Fprintf(out, "BookBridge GUI on http://%s\n", addr)
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			fmt.Fprintln(out, "web error:", err)
		}
	}()

	if cfg.Schedule == "" {
		fmt.Fprintln(out, "scheduler disabled (no schedule set)")
		select {} // block forever (GUI still serves)
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

func runWeb(args []string, getenv func(string) string, out io.Writer) int {
	st, err := store.Open(orEnv(getenv, "BB_DB", "/config/bookbridge.db"))
	if err != nil {
		fmt.Fprintln(out, "store error:", err)
		return 1
	}
	defer st.Close()
	bootstrapAdmin(st, getenv)
	srv := newWebServer(st, getenv)
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
