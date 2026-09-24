package main

import (
	"log"
	"net/http"
	"os"

	"goto/internal/testutil/mockoidc"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://mockoidc:8080"
	}

	_, handler, err := mockoidc.NewWithAddr(":"+port, publicURL)
	if err != nil {
		log.Fatalf("failed to create mock oidc server: %v", err)
	}

	log.Printf("Mock OIDC server running on :%s, public URL: %s", port, publicURL)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
