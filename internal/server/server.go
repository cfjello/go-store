package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"go-store/internal/database"
)

type Server struct {
	port int

	db database.Service
}

func NewServer() *http.Server {

	port, _ := strconv.Atoi(os.Getenv("PORT"))

	if port == 0 {
		port = 9090 // Default port if not set in environment
	}
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

	fmt.Printf("Server is running on port %d\n", NewServer.port)

	return server
}
