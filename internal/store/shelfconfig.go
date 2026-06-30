package store

import "context"

type ShelfCfg struct {
	Shelf            string
	Enabled          bool
	Format, Language string
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
