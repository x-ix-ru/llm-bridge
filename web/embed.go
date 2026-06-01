package web

import "embed"

// Static contains the embedded admin UI static files.
//
//go:embed static/index.html static/style.css static/app.js
var Static embed.FS
