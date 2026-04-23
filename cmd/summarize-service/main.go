package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"

	"distributed-logs/internal/summarize"
)

func main() {
	h := summarize.NewHandler(os.Getenv("ANTHROPIC_API_KEY"))

	r := gin.Default()
	h.RegisterRoutes(r)

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8081"
	}

	log.Printf("summarize-service listening at %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
