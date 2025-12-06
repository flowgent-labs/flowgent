// Package migration provides embedded SQL migration files.
package migration

import "embed"

// FS contains all migration SQL files organised as postgres/<version>/<name>.sql
// and sqlite/<version>/<name>.sql.
//
//go:embed postgres sqlite
var FS embed.FS
