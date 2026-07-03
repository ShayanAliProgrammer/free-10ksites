// Package assets holds all embedded static files (HTML templates, CSS, JS).
// This file lives at the project root so that go:embed directives can access
// the templates/ and static/ directories using relative paths.
//
// The package is named "assets" even though the directory is the module root.
// Import as: import "10ksites" — and reference as assets.XXX.
// Actually, Go uses the package name in the import, not the directory name.
// Since this is at the module root, the import path is "10ksites".
// The package name "assets" is what you use to reference exported identifiers.
package assets

import "embed"

//go:embed templates/*.html
var TemplatesFS embed.FS

//go:embed static/tailwind.css
var CSS []byte

//go:embed static/htmx.min.js
var HTMX []byte

//go:embed static/js/app.js
var AppJS []byte

//go:embed static/js/security.js
var SecurityJS []byte
