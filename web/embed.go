package web

import "embed"

// FS contains the bastion management console assets.
//
//go:embed index.html styles.css api.js components.js app.js main.js state.js router.js components/*.js views/*.js
var FS embed.FS
