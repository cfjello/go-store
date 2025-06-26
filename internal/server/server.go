package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/cfjello/go-store/internal/database"
	"github.com/cfjello/go-store/pkg/util"
)

type Server struct {
	port int
	db   *database.DBService
}

func NewServer() *http.Server {

	util.SetEnv() // Load default environment variables
	port, _ := strconv.Atoi(os.Getenv("PORT"))

	// Initialize the database service
	NewServer := &Server{
		port: port,
		db:   database.New(),
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
