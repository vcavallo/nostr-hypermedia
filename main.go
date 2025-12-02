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

	// Root path redirects to HTML timeline, everything else 404
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/html/timeline?kinds=1&limit=20", http.StatusFound)
		} else {
			http.NotFound(w, r)
		}
	})
	http.HandleFunc("/html/timeline", htmlTimelineHandler)
	http.HandleFunc("/html/thread/", htmlThreadHandler)
	http.HandleFunc("/html/profile/", htmlProfileHandler)
	http.HandleFunc("/html/login", htmlLoginHandler)
	http.HandleFunc("/html/logout", htmlLogoutHandler)
	http.HandleFunc("/html/post", htmlPostNoteHandler)
	http.HandleFunc("/html/reply", htmlReplyHandler)
	http.HandleFunc("/html/react", htmlReactHandler)
	http.HandleFunc("/html/check-connection", htmlCheckConnectionHandler)
	http.HandleFunc("/html/reconnect", htmlReconnectHandler)
	http.HandleFunc("/health", healthHandler)

	// Start NIP-46 connection listener for nostrconnect:// flow
	StartConnectionListener(defaultNostrConnectRelays)

	log.Printf("Starting server on :%s", port)
	log.Printf("Open http://localhost:%s in your browser", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
