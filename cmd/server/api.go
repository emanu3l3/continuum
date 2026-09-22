package main

import (
	"log"
	"net/http"
	"time"

	"github.com/emanu3l3/continuum/pkg/registry"
	"github.com/emanu3l3/continuum/pkg/storage/diskstorage"
	"github.com/emanu3l3/continuum/pkg/upload"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type config struct {
	addr               string
	baseFilePath       string
	maxFileSize        int64
	maxChunkSize       int64
	maxInMemSeconds    int64
	acceptedExtensions map[string]struct{}
}

type application struct {
	cfg config
}

func (app *application) mount() http.Handler {
	r := chi.NewRouter()
	// middleware
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Use(middleware.Heartbeat("/ping"))
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(5 * time.Second))
	r.Use(middleware.SetHeader("Cache-Control", "no-store"))

	uploadRegistry := registry.NewRegistry()
	storage := diskstorage.NewDiskStorage(app.cfg.baseFilePath)
	uploadService := upload.NewService(storage, uploadRegistry, app.cfg.maxInMemSeconds)

	uploadHandler := upload.NewHandler(uploadService, app.cfg.maxFileSize, app.cfg.maxChunkSize, app.cfg.acceptedExtensions)

	// mount uploadHandler
	r.Mount("/", uploadHandler)

	return r
}

func (app *application) run(h http.Handler) error {
	log.Printf("server started at addr %s", app.cfg.addr)

	server := &http.Server{
		Addr:    app.cfg.addr,
		Handler: h,
	}

	return server.ListenAndServe()
}
