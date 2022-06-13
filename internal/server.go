package internal

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var (
	//go:embed templates
	templates embed.FS

	galleryTemplate      = template.Must(template.ParseFS(templates, "templates/gallery.html"))
	imageDetailTemplate  = template.Must(template.ParseFS(templates, "templates/image_detail.html"))
	kaleidoscopeTemplate = template.Must(template.ParseFS(templates, "templates/kaleidoscope.html"))
	manageTemplate       = template.Must(template.ParseFS(templates, "templates/manage.html"))
)

func NewServer(storage *ImageStorage, creds map[string]string) *http.Server {

	r := chi.NewRouter()

	r.Use(middleware.Compress(5, "text/html"))

	r.Get("/", storage.galleryHandler)
	r.Get("/health", healthHandler)
	r.Get("/detail", storage.detailHandler)
	r.Get("/kaleidoscope", kaleidoscopeHandler)
	r.Get("/images/{name}", storage.getImageHandler)
	r.Mount("/images", authImageRouter(storage, creds))
	r.Mount("/manage", adminRouter(storage, creds))

	return &http.Server{
		Addr:         ":8080",
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  3 * time.Minute,
		ErrorLog:     newServerErrorLog(),
	}
}

func authImageRouter(s *ImageStorage, creds map[string]string) http.Handler {

	r := chi.NewRouter()
	r.Use(middleware.BasicAuth("admin", creds))

	r.Post("/create", s.createImageHandler)
	r.Put("/{name}", s.updateImageHandler)
	r.Delete("/{name}", s.deleteImageHandler)

	return r
}

func adminRouter(s *ImageStorage, creds map[string]string) http.Handler {

	r := chi.NewRouter()
	r.Use(middleware.BasicAuth("admin", creds))

	r.Get("/", s.manageHandler)

	return r
}

type serverErrorLogWriter struct{}

func (*serverErrorLogWriter) Write(p []byte) (int, error) {
	if !bytes.HasPrefix(p, []byte("http: TLS handshake error")) {
		fmt.Fprintln(os.Stderr, string(p))
	}
	return len(p), nil
}

func newServerErrorLog() *log.Logger {
	return log.New(&serverErrorLogWriter{}, "", 0)
}
