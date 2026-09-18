package main

// handlers_stubs.go — the 15 audit stubs were implemented in dedicated handler
// files; this file now keeps only the shared rowsAffected helper.

// rowsAffected extracts the affected-row count from a pgx Exec result.
// pgconn.CommandTag satisfies RowsAffected() int64.
func rowsAffected(tag interface{ RowsAffected() int64 }) int64 {
	return tag.RowsAffected()
}
