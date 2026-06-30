package store

import (
	"context"
	"strings"
)

// DoneUntaggedForCWA returns downloaded books (state 'done') whose shelves have
// not yet been pushed to CWA, with their shelf list, for the tagging pass.
func (s *Store) DoneUntaggedForCWA(ctx context.Context) ([]BookRow, error) {
	rows, err := s.db.QueryContext(ctx, `
	  SELECT b.source, b.external_id, b.title, b.author,
	    COALESCE(b.user_rating,0), COALESCE(b.added_at,''), COALESCE(b.reading_status,''),
	    COALESCE((SELECT GROUP_CONCAT(shelf, ',') FROM book_shelves bs
	              WHERE bs.source=b.source AND bs.external_id=b.external_id),'')
	  FROM books b
	  WHERE b.state='done' AND COALESCE(b.cwa_tagged,0)=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BookRow
	for rows.Next() {
		var b BookRow
		var shelvesCSV string
		if err := rows.Scan(&b.Source, &b.ExternalID, &b.Title, &b.Author, &b.UserRating, &b.AddedAt, &b.ReadingStatus, &shelvesCSV); err != nil {
			return nil, err
		}
		if shelvesCSV != "" {
			b.Shelves = strings.Split(shelvesCSV, ",")
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// MarkCWATagged records that a book's shelves were pushed to CWA.
func (s *Store) MarkCWATagged(ctx context.Context, source, externalID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET cwa_tagged=1 WHERE source=? AND external_id=?`, source, externalID)
	return err
}
