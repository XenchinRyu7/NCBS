package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	handler "github.com/cbs-bank/enterprise-portfolio-api/api"
	"github.com/cbs-bank/enterprise-portfolio-api/pkg/config"
)

func main() {
	cfg := config.Load()
	addr := ":" + cfg.Port

	srv := &http.Server{
		Addr:         addr,
		Handler:      http.HandlerFunc(handler.Handler),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	fmt.Printf("================================================================\n")
	fmt.Printf(" Enterprise Portfolio Core API — Running on http://localhost%s\n", addr)
	fmt.Printf(" Interactive API Playground   — http://localhost%s/docs\n", addr)
	fmt.Printf(" Health Check Endpoint        — http://localhost%s/v1/system/health\n", addr)
	fmt.Printf("================================================================\n")

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

