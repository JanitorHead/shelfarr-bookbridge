// Command dbstat prints a quick state breakdown of the BookBridge SQLite store.
// Usage: dbstat <path-to-bookbridge.db>
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: dbstat <db>")
		os.Exit(2)
	}
	db, err := sql.Open("sqlite", "file:"+os.Args[1]+"?_pragma=busy_timeout(5000)")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT state, COUNT(*) FROM books GROUP BY state ORDER BY 2 DESC`)
	if err != nil {
		panic(err)
	}
	fmt.Println("== state counts ==")
	for rows.Next() {
		var s string
		var n int
		rows.Scan(&s, &n)
		fmt.Printf("  %-12s %d\n", s, n)
	}
	rows.Close()

	var total int
	db.QueryRow(`SELECT COUNT(*) FROM books`).Scan(&total)
	fmt.Printf("  %-12s %d\n", "TOTAL", total)

	var withReq int
	db.QueryRow(`SELECT COUNT(*) FROM books WHERE shelfarr_request_id IS NOT NULL AND shelfarr_request_id <> ''`).Scan(&withReq)
	fmt.Printf("  %-12s %d\n", "has_req_id", withReq)

	fmt.Println("== not_found / parked samples ==")
	r2, _ := db.Query(`SELECT title, COALESCE(isbn10,''), attempt_count FROM books WHERE state IN ('not_found','parked') ORDER BY attempt_count DESC LIMIT 12`)
	for r2.Next() {
		var t, is string
		var a int
		r2.Scan(&t, &is, &a)
		fmt.Printf("  [att=%d] %-40s isbn=%s\n", a, t, is)
	}
	r2.Close()
}
