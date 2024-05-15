// cmd/server/main.go
package main

import (
	"log"
	"telegram_moderator/internal/config"
	"telegram_moderator/internal/http"
)

func main() {
	config.LoadEnv()

	port := config.GetEnv("LOCAL_PORT_FOR_WEBHOOK", "8443")
	log.Printf("Starting server on :%s", port)

	http.StartServer(port)
}
