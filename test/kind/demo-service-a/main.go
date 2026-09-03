// Command demo-service-a is a minimal HTTP server used to exercise tracer
// end-to-end: it generates a trace_id per request, logs JSON to stdout,
// calls demo-service-b passing the trace_id along, and logs completion
// with a duration_ms field to exercise the trace view's duration chip.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func genTraceID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func logJSON(fields map[string]any) {
	b, _ := json.Marshal(fields)
	fmt.Println(string(b))
}

func main() {
	port := getEnv("PORT", "8080")
	serviceBURL := getEnv("SERVICE_B_URL", "http://localhost:8081")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		traceID := genTraceID()

		logJSON(map[string]any{
			"level":    "info",
			"msg":      "received request",
			"trace_id": traceID,
			"service":  "svc-a",
			"path":     r.URL.Path,
		})

		req, _ := http.NewRequest("GET", serviceBURL+"/", nil)
		req.Header.Set("X-Trace-Id", traceID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logJSON(map[string]any{
				"level":    "error",
				"msg":      "call to svc-b failed",
				"trace_id": traceID,
				"service":  "svc-a",
				"error":    err.Error(),
			})
			http.Error(w, "upstream error", http.StatusBadGateway)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		logJSON(map[string]any{
			"level":       "info",
			"msg":         "request completed",
			"trace_id":    traceID,
			"service":     "svc-a",
			"duration_ms": time.Since(start).Milliseconds(),
		})

		w.Write([]byte("ok trace=" + traceID + "\n"))
	})

	log.Printf("demo-service-a listening on :%s, calling svc-b at %s", port, serviceBURL)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
