package api

import (
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/AsCat/jellyhere/server/internal/bridge"
)

var mcpTraceSequence atomic.Uint64

// serveMCPWithLog records request timing and links it to the device call log.
// It deliberately does not inspect MCP arguments, screen content, or responses.
func serveMCPWithLog(w http.ResponseWriter, r *http.Request, clientRef string, handler http.Handler) {
	trace := strconv.FormatUint(mcpTraceSequence.Add(1), 36)
	started := time.Now()
	defer func() {
		log.Printf("mcp_request trace=%s client_ref=%s method=%s total_ms=%d",
			trace, clientRef, r.Method, time.Since(started).Milliseconds())
	}()
	handler.ServeHTTP(w, r.WithContext(bridge.WithCallTrace(r.Context(), trace)))
}
