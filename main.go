package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"go.etcd.io/bbolt"
)

var db *bbolt.DB

// URLData stores the original URL, expiration time, and click count.
type URLData struct {
	OriginalURL string    `json:"original_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	Clicks      int       `json:"clicks"`
}

// Base62 characters for short key generation
const base62Chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func main() {
	// Open BoltDB
	var err error
	db, err = bbolt.Open("urls.db", 0600, nil)
	if err != nil {
		log.Fatal("Failed to open DB:", err)
	}
	defer db.Close()

	// Create the 'urls' bucket
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("urls"))
		return err
	})
	if err != nil {
		log.Fatal("Failed to create DB bucket:", err)
	}

	// Serve static files (CSS/JS)
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// Route Handlers
	http.HandleFunc("/", homeHandler)           // Homepage
	http.HandleFunc("/shorten", shortenURL)     // URL Shortener API
	http.HandleFunc("/{shortKey}", redirectURL) // Dynamic route for redirects

	// Start the server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("🚀 Server started on: http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// Home Page Handler
func homeHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("static/index.html"))
	tmpl.Execute(w, nil)
}

// Shorten URL Handler
func shortenURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	originalURL := r.FormValue("url")
	customKey := r.FormValue("custom_key")
	expiration := r.FormValue("expiration")

	if originalURL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	// Generate or validate custom key
	if customKey == "" {
		customKey = generateShortKey()
	} else {
		err := db.View(func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte("urls"))
			if b.Get([]byte(customKey)) != nil {
				return fmt.Errorf("custom key already exists")
			}
			return nil
		})
		if err != nil {
			http.Error(w, "Custom key already exists", http.StatusBadRequest)
			return
		}
	}

	// Parse expiration date
	var expiresAt time.Time
	if expiration != "" {
		expiresAt, err := time.Parse("2006-01-02", expiration)
		if err != nil {
			http.Error(w, "Invalid date format (YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
	} else {
		expiresAt = time.Now().AddDate(0, 0, 7) // Default: 7 days
	}

	// Store URL data
	urlData := URLData{OriginalURL: originalURL, ExpiresAt: expiresAt, Clicks: 0}
	jsonData, err := json.Marshal(urlData)
	if err != nil {
		http.Error(w, "Failed to store URL", http.StatusInternalServerError)
		return
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("urls"))
		return b.Put([]byte(customKey), jsonData)
	})
	if err != nil {
		http.Error(w, "Failed to store URL", http.StatusInternalServerError)
		return
	}

	shortURL := fmt.Sprintf("http://localhost:8080/%s", customKey)
	fmt.Fprintf(w, "Short URL: %s", shortURL)
}

// Redirect Handler
func redirectURL(w http.ResponseWriter, r *http.Request) {
	shortKey := r.URL.Path[1:]

	var urlData URLData
	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("urls"))
		data := b.Get([]byte(shortKey))
		if data == nil {
			return fmt.Errorf("URL not found")
		}
		return json.Unmarshal(data, &urlData)
	})
	if err != nil {
		http.Error(w, "URL not found", http.StatusNotFound)
		return
	}

	// Check expiration
	if time.Now().After(urlData.ExpiresAt) {
		http.Error(w, "URL has expired", http.StatusGone)
		return
	}

	// Increment click count
	err = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("urls"))
		urlData.Clicks++
		jsonData, err := json.Marshal(urlData)
		if err != nil {
			return err
		}
		return b.Put([]byte(shortKey), jsonData)
	})
	if err != nil {
		http.Error(w, "Failed to update analytics", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, urlData.OriginalURL, http.StatusFound)
}

// Generate Short Key
func generateShortKey() string {
	rand.Seed(time.Now().UnixNano())
	shortKey := make([]byte, 6)
	for i := range shortKey {
		shortKey[i] = base62Chars[rand.Intn(len(base62Chars))]
	}
	return string(shortKey)
}
