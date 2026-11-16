package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/AsCat/jellyhere/server/internal/bridge"
	"github.com/gorilla/websocket"
)

func main() {
	url := env("JELLYHERE_DEVICE_URL", "ORBBUDDY_DEVICE_URL")
	token := env("JELLYHERE_DEVICE_TOKEN", "ORBBUDDY_DEVICE_TOKEN")
	if url == "" || token == "" {
		log.Fatal("JELLYHERE_DEVICE_URL and JELLYHERE_DEVICE_TOKEN are required (ORBBUDDY_* aliases remain supported)")
	}
	for {
		header := http.Header{"Authorization": []string{"Bearer " + token}}
		conn, _, err := websocket.DefaultDialer.Dial(url, header)
		if err != nil {
			log.Printf("connect: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}
		log.Print("connected as mock device")
		for {
			var command bridge.Command
			if err := conn.ReadJSON(&command); err != nil {
				log.Printf("disconnected: %v", err)
				break
			}
			result := map[string]any{"done": true, "mock": true}
			if command.Method == "get_screen" {
				result = map[string]any{"revision": 1, "width": 1080, "height": 2400,
					"tree": map[string]any{"path": "0", "text": "JellyHere mock device", "children": []any{}},
				}
			}
			data, _ := json.Marshal(result)
			if err := conn.WriteJSON(bridge.Response{ID: command.ID, OK: true, Result: data}); err != nil {
				break
			}
		}
		_ = conn.Close()
		time.Sleep(3 * time.Second)
	}
}

func env(primary, legacy string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	return os.Getenv(legacy)
}
