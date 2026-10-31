package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cfjello/go-store/internal/database"
	"github.com/cfjello/go-store/pkg/config"
	"github.com/cfjello/go-store/pkg/job"
	"github.com/cfjello/go-store/pkg/perf"
	"github.com/cfjello/go-store/pkg/store"
	"github.com/cfjello/go-store/pkg/util"
)

type Server struct {
	port  int
	db    *database.DBService
	store *store.Store
	jobs  *job.JobService
	rest  config.RestAPI
}

func NewServer() *http.Server {

	util.SetEnv() // Load default environment variables
	perf.SyncFromEnv()
	perf.StartReporter()
	port, _ := strconv.Atoi(os.Getenv("PORT"))

	// Initialize the database, store, and job services
	db := database.New(false)
	st := store.New(db, false)

	// Initialize the server
	NewServer := &Server{
		port:  port,
		db:    db,
		store: st,
		jobs:  job.NewJobService(st),
		rest:  loadRestConfig(),
	}

	// Declare Server config
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", NewServer.port),
		Handler:      NewServer.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Graceful shutdown handler
	go func() {
		// Wait for interrupt signal to gracefully shutdown the server
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c

		// Create a deadline for shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Attempt to gracefully shutdown the database
		if err := NewServer.db.Close(); err != nil {
			fmt.Printf("Database forced shutdown: %v\n", err)
		}
		// Attempt to gracefully shutdown
		if err := server.Shutdown(ctx); err != nil {
			fmt.Printf("Server forced shutdown: %v\n", err)
		}

		fmt.Println("Server gracefully shutdown")
		os.Exit(0)
	}()

	fmt.Printf("Server is running on port %d\n", NewServer.port)

	return server
}

// loadRestConfig starts from the configured REST defaults and applies the
// environment overrides (REST_ENABLE_WRITES, REST_MAX_BODY_BYTES,
// CORS_ALLOWED_ORIGINS). util.SetEnv has already populated unset variables
// from DefaultEnv, so the defaults here are a second safety net.
func loadRestConfig() config.RestAPI {
	cfg := config.DefaultConfig().RestAPI
	switch strings.ToLower(strings.TrimSpace(os.Getenv("REST_ENABLE_WRITES"))) {
	case "true", "1", "yes":
		cfg.EnableWrites = true
	}
	if v := strings.TrimSpace(os.Getenv("REST_MAX_BODY_BYTES")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cfg.MaxBodyBytes = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS")); v != "" {
		cfg.AllowedOrigins = v
	}
	return cfg
}
