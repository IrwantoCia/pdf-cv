package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/IrwantoCia/pdf-cv/internal/cv"
	"github.com/IrwantoCia/pdf-cv/internal/pdf"
	"github.com/IrwantoCia/pdf-cv/internal/pdfqueue"
	"github.com/IrwantoCia/pdf-cv/internal/server"
	_ "modernc.org/sqlite"
)

func main() {
	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	store := cv.NewStore("data/cv")
	renderer, err := pdf.NewRenderer("web/tex/cv.tex.tmpl")
	if err != nil {
		log.Fatalf("create pdf renderer: %v", err)
	}

	generator, err := pdf.NewGenerator(renderer, 20*time.Second)
	if err != nil {
		log.Fatalf("create pdf generator: %v", err)
	}

	if err := os.MkdirAll("data/queue", 0o700); err != nil {
		log.Fatalf("create queue directory: %v", err)
	}

	db, err := sql.Open("sqlite", filepath.Join("data", "queue", "jobs.db"))
	if err != nil {
		log.Fatalf("open queue database: %v", err)
	}
	defer db.Close()

	queueSvc, err := pdfqueue.NewService(pdfqueue.Config{
		DB:           db,
		Generator:    generator,
		WorkRoot:     filepath.Join("data", "build", "jobs"),
		QueueLimit:   20,
		ReadyTTL:     2 * time.Minute,
		FailedTTL:    2 * time.Minute,
		PollInterval: time.Second,
	})
	if err != nil {
		log.Fatalf("create pdf queue service: %v", err)
	}
	if err := queueSvc.Start(context.Background()); err != nil {
		log.Fatalf("start pdf queue service: %v", err)
	}

	handler, err := server.NewHandler(store, queueSvc)
	if err != nil {
		log.Fatalf("create handler: %v", err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("server listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}
