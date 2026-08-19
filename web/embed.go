package web

import "embed"

// Static contains the self-contained browser application.
//
//go:embed dist
var Static embed.FS
