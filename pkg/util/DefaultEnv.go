package util

import (
	"os"

	config "github.com/cfjello/go-store/pkg/config"
)

func SetEnv() {
	// Load configuration defaults
	defaultConfig := config.DefaultConfig()

	// Set environment variables from DefaultEnv if not already set
	for key, value := range defaultConfig.DefaultEnv {
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
