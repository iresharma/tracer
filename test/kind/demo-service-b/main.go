// Command demo-service-b is the downstream half of the tracer e2e demo
// pair: it reads the trace_id demo-service-a passed via header and logs a
// JSON line carrying the same trace_id, so tracer can stitch the two
// services' logs together after the fact — tracer never propagates trace
// context itself, applications own that convention.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func logJSON(fields map[string]any) {
	b, _ := json.Marshal(fields)
	fmt.Println(string(b))
}

func main() {
	port := getEnv("PORT", "8081")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		traceID := r.Header.Get("X-Trace-Id")

		logJSON(map[string]any{
			"level":    "info",
			"msg":      "received request",
			"trace_id": traceID,
			"service":  "svc-b",
		})

		// Simulate a little work.
		time.Sleep(5 * time.Millisecond)

		logJSON(map[string]any{
			"level":       "info",
			"msg":         "request processed",
			"trace_id":    traceID,
			"service":     "svc-b",
			"duration_ms": time.Since(start).Milliseconds(),
		})

		w.Write([]byte("ok\n"))
	})

	log.Printf("demo-service-b listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
