package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
)

type SearchResult struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Summary     string  `json:"summary"`
	PublishedAt string  `json:"published_at"`
	Rank        float64 `json:"rank"`
}

var db *sql.DB

func initDB() error {
	var err error

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Println("DATABASE_URL not set")
		return nil
	}

	db, err = sql.Open("postgres", connStr)
	if err != nil {
		return err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	return db.Ping()
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	query := r.URL.Query().Get("q")
	siteCode := r.URL.Query().Get("site_code")
	if siteCode == "" {
		siteCode = "main"
	}

	if query == "" {
		_ = json.NewEncoder(w).Encode([]SearchResult{})
		return
	}

	if db == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	rows, err := db.Query(`
		SELECT
			id,
			title,
			url,
			summary,
			published_at::text,
			ts_rank_cd(search_tsv, websearch_to_tsquery('simple', $1)) AS rank
		FROM hugo_pages
		WHERE
			site_code = $2
			AND (
				search_tsv @@ websearch_to_tsquery('simple', $1)
				OR lower(title) LIKE '%' || lower($1) || '%'
				OR similarity(lower(title), lower($1)) > 0.25
			)
		ORDER BY rank DESC, published_at DESC
		LIMIT 8
	`, query, siteCode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	results := []SearchResult{}
	for rows.Next() {
		var s SearchResult
		if err := rows.Scan(&s.ID, &s.Title, &s.URL, &s.Summary, &s.PublishedAt, &s.Rank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		results = append(results, s)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(results)
}

func main() {
	if err := initDB(); err != nil {
		log.Printf("Database not available: %v", err)
		log.Println("Server starting anyway — search will fail until DB is ready")
	}
	if db != nil {
		defer db.Close()
	}

	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/search", searchHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Search API listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
