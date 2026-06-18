package web

import "embed"

// FS contains the bastion management console assets.
//
//go:embed index.html styles.css api.js components.js app.js
var FS embed.FS
