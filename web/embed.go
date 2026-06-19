package web

import "embed"

// FS contains the bastion management console assets.
//
//go:embed index.html styles.css api.js i18n.js theme.js main.js state.js router.js tag-colors.js components/*.js views/*.js
var FS embed.FS
