package api

import (
	_ "embed"
	"net/http"
)

//go:embed brand-icon.svg
var brandIconSVG []byte

func registerBrandIcon(mux *http.ServeMux) {
	mux.HandleFunc("GET /brand-icon.svg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(brandIconSVG)
	})
}
