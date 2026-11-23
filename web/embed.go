// Package web embeds the built frontend.
package web

import "embed"

// Dist holds web/dist. The build works when it contains only .gitkeep.
//
//go:embed all:dist
var Dist embed.FS
