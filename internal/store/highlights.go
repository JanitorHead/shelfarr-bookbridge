package store

import (
	"context"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

// ReplaceHighlights swaps a book's stored Kindle highlights for a fresh set
// (delete-then-insert, so a re-scrape stays in sync). No-op on an empty set so a
// transient scrape failure never wipes existing highlights.
func (s *Store) ReplaceHighlights(ctx context.Context, source, externalID string, hs []sources.Highlight) error {
	if len(hs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM book_highlights WHERE source=? AND external_id=?`, source, externalID); err != nil {
		return err
	}
	for i, h := range hs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO book_highlights(source,external_id,position,location,text,note) VALUES(?,?,?,?,?,?)`,
			source, externalID, i, h.Location, h.Text, h.Note); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// HighlightsFor returns a book's highlights in order.
func (s *Store) HighlightsFor(ctx context.Context, source, externalID string) ([]sources.Highlight, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT location, text, note FROM book_highlights WHERE source=? AND external_id=? ORDER BY position`,
		source, externalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sources.Highlight
	for rows.Next() {
		var h sources.Highlight
		if err := rows.Scan(&h.Location, &h.Text, &h.Note); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
