package views

import "embed"

//go:embed css/tailwind.css
var TailwindCSS string

//go:embed all:web/dist
var WebFS embed.FS
