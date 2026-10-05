package migrations

import "embed"

// Files contains the database migrations bundled with the server binary.
//
//go:embed *.sql
var Files embed.FS
