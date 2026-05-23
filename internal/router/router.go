package router

import (
	"embed"
	"io/fs"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"local-storefront-engine/internal/handlers"
)

var originSecret = os.Getenv("ORIGIN_SECRET")

func NewRouter(h *handlers.Handler, embeddedFiles embed.FS) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(enforceOriginSecret)

	// ── HOME PAGE INTERCEPTOR ─────────────────────────────────────────────
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		indexBytes, err := embeddedFiles.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "Frontend root asset not found in embed tree", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(indexBytes)
	})

	// ── STYLESHEET INTERCEPTOR ────────────────────────────────────────────
	// Intercepts the root style.css request and serves it safely from public
	r.Get("/style.css", func(w http.ResponseWriter, r *http.Request) {
		cssBytes, err := embeddedFiles.ReadFile("web/public/style.css")
		if err != nil {
			http.Error(w, "Stylesheet not found in embed tree", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cssBytes)
	})

	// ── PUBLIC API ENDPOINTS ───────────────────────────────────────────────
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/products", h.GetProductsHandler)
		r.Post("/orders", h.PlaceOrderHandler)
		r.Get("/orders", h.GetUserOrdersHandler)

		r.Patch("/admin/inventory", h.AdminUpdateInventoryHandler)
	})

	// ── STATIC EMBEDDED PATH MAPPINGS ──────────────────────────────────────
	mountStaticDir(r, embeddedFiles, "/public", "web/public")
	mountStaticDir(r, embeddedFiles, "/static", "web/static")

	return r
}

func enforceOriginSecret(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if originSecret != "" && r.Header.Get("X-Origin-Secret") != originSecret {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mountStaticDir(r chi.Router, embeddedFiles embed.FS, urlPrefix, fsRoot string) {
	sub, err := fs.Sub(embeddedFiles, fsRoot)
	if err != nil {
		return
	}

	fileServer := http.FileServer(http.FS(sub))

	r.Handle(
		urlPrefix+"/*",
		http.StripPrefix(urlPrefix, fileServer),
	)
}
