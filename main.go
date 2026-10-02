package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var db *sql.DB

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func main() {
	databaseURL, err := loadDatabaseConfig("database.json")
	if err != nil {
		log.Fatalf("load database configuration: %v", err)
	}

	db, err = sql.Open("pgx", databaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS travel`); err != nil {
		log.Fatalf("create travel schema: %v", err)
	}
	if err := createAPRRuleTable(ctx, db); err != nil {
		log.Fatalf("create ZTB_APR_RULE table: %v", err)
	}
	if err := createCodeTTable(ctx, db); err != nil {
		log.Fatalf("create ZTB_CODE_T table: %v", err)
	}

	if err := createConfigTTable(ctx, db); err != nil {
		log.Fatalf("create ZTB_CONFIG table: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/apr-rules", aprRulesHandler)
	mux.HandleFunc("/api/code-t", codeTHandler)
	mux.HandleFunc("/api/config-t", configTHandler)

	server := &http.Server{
		Addr:              ":8081",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("Server running on :8081")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
