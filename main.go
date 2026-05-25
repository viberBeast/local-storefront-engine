package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"local-storefront-engine/internal/handlers"
	"local-storefront-engine/internal/storage"
)

//go:embed web/*
var embeddedFiles embed.FS

func main() {
	log.Println("Starting Dual-Channel Storefront Security Manager...")

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "storefront.db"
	}
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		adminToken = "super-secret-dev-token"
	}

	publicPort := os.Getenv("PUBLIC_PORT")
	if publicPort == "" {
		publicPort = "8080"
	}
	privatePort := os.Getenv("PRIVATE_PORT")
	if privatePort == "" {
		privatePort = "8081"
	}

	db, err := storage.Open(context.Background(), dbPath)
	if err != nil {
		log.Fatalf("Fatal database initialization error: %v", err)
	}
	defer db.Close()
	log.Printf("SQLite layer active at: %s", dbPath)

	seedDatabaseIfPristine(db)

	h, err := handlers.NewHandler(db, adminToken)
	if err != nil {
		log.Fatalf("Fatal handler instantiation error: %v", err)
	}

	webFS, err := fs.Sub(embeddedFiles, "web")
	if err != nil {
		log.Fatalf("Fatal embedded root sub-tree generation mismatch: %v", err)
	}
	staticFileServer := http.FileServer(http.FS(webFS))

	// ==========================================
	// 1. PUBLIC ROUTER DEFINITION (Customer facing)
	// ==========================================
	publicMux := http.NewServeMux()
	
	publicMux.Handle("/", staticFileServer)
	publicMux.Handle("/static/", staticFileServer)
	publicMux.Handle("/images/", staticFileServer)
	
	publicMux.HandleFunc("/api/products", h.GetProductsHandler)
	publicMux.HandleFunc("/api/checkout", h.PlaceOrderHandler)
	publicMux.HandleFunc("/api/orders", h.GetUserOrdersHandler)

	publicServer := &http.Server{
		Addr:         "0.0.0.0:" + publicPort,
		Handler:      publicMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// ==========================================
	// 2. PRIVATE ROUTER DEFINITION (LAN-locked Admin)
	// ==========================================
	privateMux := http.NewServeMux()
	
	privateMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		adminHTML, err := embeddedFiles.ReadFile("web/admin.html")
		if err != nil {
			http.Error(w, "Administrative assets payload missing internal embed layout", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(adminHTML)
	})

	// Mount the handler locally on port 8081 to bypass CORS entirely
	privateMux.HandleFunc("/api/products", h.GetProductsHandler)
	privateMux.HandleFunc("/admin/api/inventory", h.AdminUpdateInventoryHandler)

	privateServer := &http.Server{
		Addr:         "127.0.0.1:" + privatePort,
		Handler:      privateMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("[PUBLIC ACCESS ONLINE] Storefront serving on http://0.0.0.0:%s", publicPort)
		if err := publicServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Critical Public Server network failure: %v", err)
		}
	}()

	go func() {
		log.Printf("[PRIVATE ADMIN ONLINE] Core control interface active at http://127.0.0.1:%s", privatePort)
		if err := privateServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Critical Private Server network failure: %v", err)
		}
	}()

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)
	<-shutdownChan
	
	log.Println("Intercept caught. Shutting down active web pools safely...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = publicServer.Shutdown(ctx)
	_ = privateServer.Shutdown(ctx)

	log.Println("Both network engine channels terminated cleanly.")
}

func seedDatabaseIfPristine(db *storage.DB) {
	products, err := db.GetAllProducts()
	if err != nil || len(products) > 0 {
		return
	}

	log.Println("Empty database discovered! Inserting brutalist catalog essentials...")

	seeds := []storage.Product{
		{ID: "p1", Name: "Brutalist Steel Chair", Description: "Raw sandblasted raw steel framing. Ergonomically uncompromising.", Price: 45000, Stock: 10, Category: "ACCESSORIES"},
		{ID: "p2", Name: "Concrete Desk Lamp", Description: "Cast basaltic concrete block housing a single high-temperature amber halogen beam.", Price: 12000, Stock: 24, Category: "ACCESSORIES"},
		{ID: "p3", Name: "Monolithic Wool Rug", Description: "Heavy-gauge un-dyed basalt gray wool weave. Heavy weave density.", Price: 8500, Stock: 5, Category: "ACCESSORIES"},
	}

	for _, item := range seeds {
		if err := db.CreateProduct(item); err != nil {
			log.Printf("Warning: Failed to seed product %s: %v", item.ID, err)
		}
	}
}
