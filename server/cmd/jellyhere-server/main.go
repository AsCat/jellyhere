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
	addr := env("JELLYHERE_LISTEN", "ORBBUDDY_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	var handler http.Handler
	if statePath := os.Getenv("JELLYHERE_MANAGED_STATE"); statePath != "" {
		var err error
		handler, err = api.NewManagedHandler(api.ManagedConfig{StatePath: statePath,
			OperatorToken:      os.Getenv("JELLYHERE_OPERATOR_TOKEN"),
			PublicURL:          os.Getenv("JELLYHERE_PUBLIC_URL"),
			GitHubClientID:     os.Getenv("JELLYHERE_GITHUB_CLIENT_ID"),
			GitHubClientSecret: os.Getenv("JELLYHERE_GITHUB_CLIENT_SECRET"),
			SMTPHost:           os.Getenv("JELLYHERE_SMTP_HOST"), SMTPPort: os.Getenv("JELLYHERE_SMTP_PORT"),
			SMTPUser: os.Getenv("JELLYHERE_SMTP_USER"), SMTPPassword: os.Getenv("JELLYHERE_SMTP_PASSWORD"),
			SMTPFrom: os.Getenv("JELLYHERE_SMTP_FROM")})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("JellyHub managed pilot listening on %s", addr)
	} else {
		cfg := api.Config{DeviceToken: env("JELLYHERE_DEVICE_TOKEN", "ORBBUDDY_DEVICE_TOKEN"), MCPToken: env("JELLYHERE_MCP_TOKEN", "ORBBUDDY_MCP_TOKEN")}
		if cfg.DeviceToken == "" || cfg.MCPToken == "" {
			log.Fatal("JELLYHERE_DEVICE_TOKEN and JELLYHERE_MCP_TOKEN are required (ORBBUDDY_* aliases remain supported)")
		}
		handler = api.NewHandler(bridge.NewHub(), cfg)
		log.Printf("JellyHere self-hosted relay listening on %s", addr)
	}
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func env(primary, legacy string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	return os.Getenv(legacy)
}
