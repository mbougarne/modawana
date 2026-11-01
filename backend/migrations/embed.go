// Package migrations contains append-only, checksummed application migrations.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
