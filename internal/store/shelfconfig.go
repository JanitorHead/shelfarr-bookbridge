package store

import "context"

type ShelfCfg struct {
	Shelf            string
	Name             string
	Count            int
	Enabled          bool
	Format, Language string
}

// UpsertDiscoveredShelf records a shelf found via source discovery, refreshing
// its display name + count while preserving the user's enabled/format/language.
// New shelves start disabled so discovery never silently changes what is synced.
func (s *Store) UpsertDiscoveredShelf(ctx context.Context, slug, name string, count int) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO shelf_config(shelf, enabled, name, book_count) VALUES(?,0,?,?)
		 ON CONFLICT(shelf) DO UPDATE SET name=excluded.name, book_count=excluded.book_count`,
		slug, name, count)
	return err
}

// AllShelfConfigs returns every known shelf (enabled first, then by name).
func (s *Store) AllShelfConfigs(ctx context.Context) ([]ShelfCfg, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT shelf, COALESCE(name,''), COALESCE(book_count,0), enabled, COALESCE(format,''), COALESCE(language,'')
		 FROM shelf_config ORDER BY enabled DESC, name COLLATE NOCASE, shelf`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShelfCfg
	for rows.Next() {
		var c ShelfCfg
		var en int
		if err := rows.Scan(&c.Shelf, &c.Name, &c.Count, &en, &c.Format, &c.Language); err != nil {
			return nil, err
		}
		c.Enabled = en != 0
		if c.Name == "" {
			c.Name = c.Shelf
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ShelvesToSync is the authoritative set of shelves to sync: once any shelves
// have been discovered/configured, the enabled toggles win; otherwise it falls
// back to the legacy comma-separated SHELVES setting.
func (s *Store) ShelvesToSync(ctx context.Context, fallback []string) ([]string, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shelf_config`).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return fallback, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT shelf FROM shelf_config WHERE enabled=1 ORDER BY shelf`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sh string
		if err := rows.Scan(&sh); err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func (s *Store) SetShelfConfig(ctx context.Context, shelf string, enabled bool, format, language string) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO shelf_config(shelf,enabled,format,language) VALUES(?,?,?,?)
		 ON CONFLICT(shelf) DO UPDATE SET enabled=excluded.enabled, format=excluded.format, language=excluded.language`,
		shelf, en, format, language)
	return err
}

// ShelfConfigs returns config for each configured shelf (defaults: enabled, no overrides).
func (s *Store) ShelfConfigs(ctx context.Context, configured []string) ([]ShelfCfg, error) {
	out := make([]ShelfCfg, 0, len(configured))
	for _, sh := range configured {
		c := ShelfCfg{Shelf: sh, Enabled: true}
		var en int
		var f, l *string
		err := s.db.QueryRowContext(ctx, `SELECT enabled, format, language FROM shelf_config WHERE shelf=?`, sh).Scan(&en, &f, &l)
		if err == nil {
			c.Enabled = en != 0
			if f != nil {
				c.Format = *f
			}
			if l != nil {
				c.Language = *l
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) EnabledShelves(ctx context.Context, configured []string) ([]string, error) {
	cfgs, err := s.ShelfConfigs(ctx, configured)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range cfgs {
		if c.Enabled {
			out = append(out, c.Shelf)
		}
	}
	return out, nil
}

func (s *Store) ShelfFormat(ctx context.Context, shelf string) (string, bool) {
	var f *string
	if err := s.db.QueryRowContext(ctx, `SELECT format FROM shelf_config WHERE shelf=?`, shelf).Scan(&f); err != nil {
		return "", false
	}
	if f == nil || *f == "" {
		return "", false
	}
	return *f, true
}
