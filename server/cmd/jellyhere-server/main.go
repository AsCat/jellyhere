package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/AsCat/jellyhere/server/internal/api"
	"github.com/AsCat/jellyhere/server/internal/bridge"
)

func main() {
	cfg := api.Config{DeviceToken: env("JELLYHERE_DEVICE_TOKEN", "ORBBUDDY_DEVICE_TOKEN"), MCPToken: env("JELLYHERE_MCP_TOKEN", "ORBBUDDY_MCP_TOKEN")}
	if cfg.DeviceToken == "" || cfg.MCPToken == "" {
		log.Fatal("JELLYHERE_DEVICE_TOKEN and JELLYHERE_MCP_TOKEN are required (ORBBUDDY_* aliases remain supported)")
	}
	addr := env("JELLYHERE_LISTEN", "ORBBUDDY_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	srv := &http.Server{Addr: addr, Handler: api.NewHandler(bridge.NewHub(), cfg), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("JellyHere listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}

func env(primary, legacy string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	return os.Getenv(legacy)
}
