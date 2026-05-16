package main

import (
	"log"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/router"
)

func main() {
	cfg := config.Load()
	r := router.New(cfg)

	addr := ":" + cfg.Port
	log.Printf("offbook backend listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
