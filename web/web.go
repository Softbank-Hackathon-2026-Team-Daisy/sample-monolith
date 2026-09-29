// Package web embeds the HelloCalc browser UI into the server binary.
package web

import "embed"

// Assets holds the static UI files served at the site root.
//
//go:embed index.html app.js styles.css
var Assets embed.FS
