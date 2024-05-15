// internal/config/config.go
package config

import (
	"log"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

func LoadEnv() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Error getting current working directory: %v", err)
	}

	envPath := filepath.Join(cwd, ".env")
	if err := godotenv.Load(
		envPath,
	); err != nil {
		log.Fatalf("Error loading .env file from %s: %v", envPath, err)
	}
}

func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}
