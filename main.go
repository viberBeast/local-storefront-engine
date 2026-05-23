package main

import (
	"context"
	"database/sql"
	"embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"local-storefront-engine/internal/handlers"
	"local-storefront-engine/internal/router"
	"local-storefront-engine/internal/storage"
)

//go:embed web/*
var embeddedFiles embed.FS

func main() {
	log.Println("Starting Storefront Engine Lifecycle Manager...")

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "storefront.db"
	}
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		adminToken = "super-secret-dev-token"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	db, err := storage.InitDB(dbPath)
	if err != nil {
		log.Fatalf("Fatal database initialization error: %v", err)
	}
	defer db.Close()
	log.Printf("SQLite storage layer initialized successfully at: %s", dbPath)

	seedDatabaseIfPristine(db)

	h, err := handlers.NewHandler(db, adminToken)
	if err != nil {
		log.Fatalf("Fatal handler instantiation error: %v", err)
	}

	appRouter := router.NewRouter(h, embeddedFiles)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      appRouter,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Storefront Engine online! Point your browser to http://localhost:%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Critical network listening issue: %v", err)
		}
	}()

	<-shutdownChan
	log.Println("Shutdown signal received. Wrapping up ongoing tasks and closing database pools gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to close with active leaks: %v", err)
	}

	log.Println("Storefront runtime closed down cleanly.")
}

func seedDatabaseIfPristine(db *sql.DB) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM products").Scan(&count)
	if err != nil || count > 0 {
		return
	}

	log.Println("Empty database discovered! Inserting brutalist catalog essentials...")

	seeds := []struct {
		ID    string
		Name  string
		Desc  string
		Price int
		Stock int
	}{
		{"p1", "Brutalist Steel Chair", "Raw sandblasted raw steel framing. Ergonomically uncompromising.", 45000, 10},
		{"p2", "Concrete Desk Lamp", "Cast basaltic concrete block housing a single high-temperature amber halogen beam.", 12000, 24},
		{"p3", "Monolithic Wool Rug", "Heavy-gauge un-dyed basalt gray wool weave. Heavy weave density.", 8500, 5},
	}

	for _, item := range seeds {
		_, _ = db.Exec(`
			INSERT INTO products (id, name, description, price, stock) 
			VALUES (?, ?, ?, ?, ?)`,
			item.ID, item.Name, item.Desc, item.Price, item.Stock,
		)
	}
}
