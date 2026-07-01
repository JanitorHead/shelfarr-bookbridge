package store

import "context"

// BackfillCovers fills a cover for any book that lacks one but has an ISBN, using
// Open Library's cover service (keyed by ISBN, hotlinkable, no API key). Source
// covers (Goodreads/Hardcover) always win because this only touches empty rows;
// it is idempotent and cheap. Returns how many rows were filled.
func (s *Store) BackfillCovers(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `
	  UPDATE books
	     SET cover_url = 'https://covers.openlibrary.org/b/isbn/' || isbn10 || '-L.jpg'
	   WHERE COALESCE(cover_url,'') = '' AND COALESCE(isbn10,'') <> ''`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
