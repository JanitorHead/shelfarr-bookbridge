package store

import (
	"context"
	"strings"
)

// AllShelfSlugs returns every known shelf slug — the catalog ingests them all,
// not just the download-enabled ones.
func (s *Store) AllShelfSlugs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT shelf FROM shelf_config ORDER BY shelf`)
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

// PromoteDownloadable moves catalog books that belong to a download-trigger shelf
// into the download queue (state 'catalog' -> 'new'); everything else stays in the
// catalog and is never requested in Shelfarr.
func (s *Store) PromoteDownloadable(ctx context.Context, downloadShelves []string) error {
	if len(downloadShelves) == 0 {
		return nil
	}
	ph := strings.TrimRight(strings.Repeat("?,", len(downloadShelves)), ",")
	args := make([]any, len(downloadShelves))
	for i, sh := range downloadShelves {
		args[i] = sh
	}
	_, err := s.db.ExecContext(ctx, `
	  UPDATE books SET state='new', updated_at=datetime('now')
	  WHERE state='catalog' AND EXISTS (
	    SELECT 1 FROM book_shelves bs
	    WHERE bs.source=books.source AND bs.external_id=books.external_id
	      AND bs.shelf IN (`+ph+`))`, args...)
	return err
}

// RefreshReadingStatus recomputes each book's reading_status from its status
// shelves (read > dnf > reading > to_read).
func (s *Store) RefreshReadingStatus(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
	UPDATE books SET reading_status = CASE
	  WHEN EXISTS(SELECT 1 FROM book_shelves bs WHERE bs.source=books.source AND bs.external_id=books.external_id AND lower(bs.shelf)='read') THEN 'read'
	  WHEN EXISTS(SELECT 1 FROM book_shelves bs WHERE bs.source=books.source AND bs.external_id=books.external_id AND lower(bs.shelf) IN ('did-not-finish','dnf')) THEN 'dnf'
	  WHEN EXISTS(SELECT 1 FROM book_shelves bs WHERE bs.source=books.source AND bs.external_id=books.external_id AND lower(bs.shelf) IN ('currently-reading','reading')) THEN 'reading'
	  WHEN EXISTS(SELECT 1 FROM book_shelves bs WHERE bs.source=books.source AND bs.external_id=books.external_id AND lower(bs.shelf) IN ('to-read','want-to-read')) THEN 'to_read'
	  ELSE reading_status END`)
	return err
}
