package main

import (
	"context"
	"errors"
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
	e.SetLogf(func(format string, a ...any) { fmt.Fprintf(os.Stdout, "[sync] "+format+"\n", a...) })
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
	srv.SetOwnershipRefresher(func(ctx context.Context) error {
		cfg, err := effectiveConfig(st, getenv)
		if err != nil {
			return err
		}
		if !cfg.CWAConfigured() {
			return fmt.Errorf("CWA is not configured — set the CWA URL, username and password in Settings")
		}
		return refreshOwnership(st, cfg, os.Stdout)
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
	if errors.Is(runErr, engine.ErrRunInProgress) {
		// Collided with an already-running sync — nothing happened, don't pollute
		// the run history with a bogus "error" entry.
		return rep, runErr
	}
	rec := store.RunRecord{
		StartedAt: start, FinishedAt: time.Now(), Mode: mode, OK: runErr == nil,
		Fetched: rep.Fetched, New: rep.New, Requested: rep.Requested, NotFound: rep.NotFound,
		Errors: rep.Errors, Summary: reportLine(mode, rep),
	}
	if runErr != nil {
		rec.ErrorText = runErr.Error()
	}
	_, _ = st.RecordRun(context.Background(), rec)
	// After a successful real run, fill any missing covers (Open Library by ISBN)
	// so the Library grid isn't full of placeholders.
	if runErr == nil && !dryRun {
		if n, _ := st.BackfillCovers(context.Background()); n > 0 {
			fmt.Fprintf(os.Stdout, "[covers] filled %d cover(s) from Open Library\n", n)
		}
		// Kindle highlights: only the Goodreads cookie source implements this; it
		// visits just the handful of annotated books, so it's cheap.
		type highlightFetcher interface {
			FetchHighlights(context.Context) (map[string][]sources.Highlight, error)
		}
		if hf, ok := sourceFor(cfg, getenv).(highlightFetcher); ok {
			if hl, err := hf.FetchHighlights(context.Background()); err != nil {
				fmt.Fprintln(os.Stdout, "[highlights]", err)
			} else {
				n := 0
				for extID, hs := range hl {
					if st.ReplaceHighlights(context.Background(), "goodreads", extID, hs) == nil {
						n += len(hs)
					}
				}
				if n > 0 {
					fmt.Fprintf(os.Stdout, "[highlights] saved %d highlight(s) across %d book(s)\n", n, len(hl))
				}
			}
		}
	}
	// Then push Goodreads shelves into CWA as tags and refresh which catalog
	// books are owned in Calibre (for the Library badges).
	if runErr == nil && !dryRun && cfg.CWAConfigured() {
		cwaTagPass(st, cfg, os.Stdout)
		if err := refreshOwnership(st, cfg, os.Stdout); err != nil {
			fmt.Fprintln(os.Stdout, "[cwa] ownership refresh:", err)
		}
	}
	return rep, runErr
}

// statusShelfName maps a reading-list (status) shelf slug to the canonical
// Calibre-Web Shelf name it should mirror into. "read" returns ok=false because
// finished books are tracked by the native read flag, not a shelf.
func statusShelfName(slug string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(slug)) {
	case "to-read", "want-to-read":
		return "To Read", true
	case "currently-reading", "reading":
		return "Currently Reading", true
	case "did-not-finish", "dnf":
		return "Did Not Finish", true
	default:
		return "", false
	}
}

// cwaTagPass syncs each downloaded ('done') book into the Calibre library via CWA
// using each field's native home: TOPIC shelves → Calibre tags (subject metadata),
// reading-list STATUS shelves → Calibre-Web Shelves (personal reading lists), and
// the "read" status → Calibre's native read flag. Rating and date-added are pushed
// too. Each book is processed at most once (cwa_tagged).
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
	shelfIDs, err := client.Shelves(ctx) // name -> id (created on demand below)
	if err != nil {
		fmt.Fprintln(out, "[cwa]", err)
		return
	}
	ensureShelf := func(name string) (int, error) {
		if id, ok := shelfIDs[name]; ok {
			return id, nil
		}
		id, err := client.CreateShelf(ctx, name)
		if err == nil {
			shelfIDs[name] = id
		}
		return id, err
	}
	done := 0
	for _, b := range pending {
		cal := bestCalibreMatch(b, lib)
		if cal == nil {
			continue // not in the Calibre library yet
		}
		// Reading-list (status) shelves → Calibre-Web Shelves, the native home for
		// a personal reading workflow. The "read" status is handled by the read
		// flag below, not as a shelf.
		for _, shelf := range b.Shelves {
			name, ok := statusShelfName(shelf)
			if !ok {
				continue
			}
			id, err := ensureShelf(name)
			if err != nil {
				fmt.Fprintln(out, "[cwa]", err)
				continue
			}
			if err := client.AddToShelf(ctx, id, cal.ID); err != nil {
				fmt.Fprintln(out, "[cwa]", err)
			}
		}
		// Topic (non-status) shelves → Calibre tags, the native home for subject
		// metadata. Status shelves never become tags.
		if topics := store.TopicTags(b.Shelves); len(topics) > 0 {
			if err := client.SetTags(ctx, cal.ID, mergeGRTags(cal.Tags, topics)); err != nil {
				fmt.Fprintln(out, "[cwa]", err)
			}
		}
		// Finished books → Calibre's native read flag (idempotent; never un-reads).
		if b.ReadingStatus == "read" {
			if err := client.MarkRead(ctx, cal.ID); err != nil {
				fmt.Fprintln(out, "[cwa]", err)
			}
		}
		if b.UserRating > 0 {
			if err := client.SetRating(ctx, cal.ID, b.UserRating); err != nil {
				fmt.Fprintln(out, "[cwa]", err)
			}
		}
		if d := dateYMD(b.AddedAt); cfg.CWADateColumn != "" && d != "" {
			if err := client.SetCustomColumn(ctx, cal.ID, cfg.CWADateColumn, d); err != nil {
				fmt.Fprintln(out, "[cwa]", err)
			}
		}
		_ = st.MarkCWATagged(ctx, b.Source, b.ExternalID)
		done++
	}
	if done > 0 {
		fmt.Fprintf(out, "[cwa] synced %d book(s) to Calibre (topic tags, reading-list shelves, read flag)\n", done)
	}
}

// bestCalibreMatch finds the Calibre book matching a tracked book by title+author
// (lenient 0.6 threshold, used for tagging books already known to be downloaded).
func bestCalibreMatch(b store.BookRow, lib []cwa.Book) *cwa.Book {
	return matchCalibre(b, lib, 0.6)
}

// matchCalibre returns the best Calibre match for b at or above threshold, else nil.
func matchCalibre(b store.BookRow, lib []cwa.Book, threshold float64) *cwa.Book {
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
	if bestScore >= threshold {
		return pick
	}
	return nil
}

// refreshOwnership cross-references the whole catalog against the Calibre (CWA)
// library so the Library can show which books the user actually owns. It uses a
// stricter match threshold than tagging (0.72) to avoid false "owned" badges.
func refreshOwnership(st *store.Store, cfg config.Config, out io.Writer) error {
	if !cfg.CWAConfigured() {
		return nil
	}
	ctx := context.Background()
	client := cwa.New(cfg.CWAURL, cfg.CWAUsername, cfg.CWAPassword)
	if err := client.Login(ctx); err != nil {
		return err
	}
	lib, err := client.ListBooks(ctx)
	if err != nil {
		return err
	}
	books, err := st.ListBooks(ctx, "", "", 1000000) // the whole catalog
	if err != nil {
		return err
	}
	if err := st.ClearOwnership(ctx); err != nil {
		return err
	}
	matched := 0
	for _, b := range books {
		if cal := matchCalibre(b, lib, 0.72); cal != nil {
			if st.SetOwnership(ctx, b.Source, b.ExternalID, cal.ID) == nil {
				matched++
			}
		}
	}
	fmt.Fprintf(out, "[cwa] ownership: %d/%d catalog books matched in Calibre\n", matched, len(books))
	return nil
}

// dateYMD returns the YYYY-MM-DD prefix of an RFC3339 string, or "" if unset.
func dateYMD(s string) string {
	if len(s) < 10 || strings.HasPrefix(s, "0001") {
		return ""
	}
	return s[:10]
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
	return fmt.Sprintf("[%s] fetched=%d new=%d requested=%d not_found=%d already_exists=%d errors=%d reconciled=%d completed=%d failed=%d rechecked=%d parked=%d retried=%d",
		mode, rep.Fetched, rep.New, rep.Requested, rep.NotFound, rep.AlreadyExists, rep.Errors,
		rep.Reconciled, rep.Completed, rep.Failed, rep.Rechecked, rep.Parked, rep.Retried)
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

	if *once {
		cycle() // a single synchronous cycle, then exit
		return 0
	}

	// Serve the GUI FIRST and ALWAYS — so the web UI is reachable immediately, even
	// while the initial sync is running and even before Shelfarr is configured.
	// (Previously the startup cycle ran synchronously and blocked the GUI for the
	// whole first sync, so the web "didn't open" after a restart.)
	go func() {
		srv := newWebServer(st, getenv)
		addr := net.JoinHostPort(cfg.GUIBind, cfg.GUIPort)
		fmt.Fprintf(out, "BookBridge GUI on http://%s\n", addr)
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			fmt.Fprintln(out, "web error:", err)
		}
	}()

	go cycle() // kick off the first sync in the background; never block the GUI

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
