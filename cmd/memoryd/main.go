package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/Pzharyuk/harness-memory/internal/api"
	"github.com/Pzharyuk/harness-memory/internal/config"
	"github.com/Pzharyuk/harness-memory/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "MEMORY_DATABASE_URL is required")
		os.Exit(1)
	}

	st, err := store.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "store: %v\n", err)
		os.Exit(1)
	}
	defer st.Pool.Close()

	if err := http.ListenAndServe(cfg.Listen, api.New(st, cfg)); err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
}
