package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Serve static files
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// API endpoints (these handle content negotiation internally)
	http.HandleFunc("/timeline", timelineHandler)
	http.HandleFunc("/thread/", threadHandler)

	// Root path serves HTML client, everything else 404
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "./static/index.html")
		} else {
			http.NotFound(w, r)
		}
	})
	http.HandleFunc("/html/timeline", htmlTimelineHandler)
	http.HandleFunc("/health", healthHandler)

	log.Printf("Starting server on :%s", port)
	log.Printf("Open http://localhost:%s in your browser (JS client)", port)
	log.Printf("Or http://localhost:%s/html/timeline?kinds=1&limit=20 (zero-JS)", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
