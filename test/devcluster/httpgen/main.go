// Command httpgen simulates a small HTTP service mesh for exercising the
// tracer dev cluster: a self-driving "gateway" originates traffic on its
// own (no external curl needed), calls "orders", which sometimes calls
// "payments". All three roles are one binary; ROLE picks the behavior so
// the same image is deployed three times with different env vars. Every
// hop logs a JSON line with a shared trace_id, so a single gateway "request"
// produces a 2-4 line trace spanning up to three pods.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
)

var paths = []string{"/api/orders", "/api/orders/checkout", "/api/cart", "/api/orders/history"}
var methods = []string{"GET", "GET", "GET", "POST"}

func logJSON(fields map[string]any) {
	b, _ := json.Marshal(fields)
	fmt.Println(string(b))
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// simulateOutcome returns a status code and an artificial extra delay,
// biased mostly toward fast 200s with occasional slow or error responses so
// the trace view has realistic variety to explore.
func simulateOutcome(errProb, slowProb float64) (status int, extraDelay time.Duration) {
	r := rand.Float64()
	switch {
	case r < errProb:
		if rand.Float64() < 0.5 {
			return 500, 0
		}
		return 404, 0
	case r < errProb+slowProb:
		return 200, time.Duration(200+rand.Intn(800)) * time.Millisecond
	default:
		return 200, 0
	}
}

func main() {
	role := getEnv("ROLE", "gateway")
	switch role {
	case "gateway":
		runGateway()
	case "orders":
		runOrders()
	case "payments":
		runPayments()
	default:
		log.Fatalf("unknown ROLE %q (want gateway|orders|payments)", role)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func runGateway() {
	port := getEnv("PORT", "8080")
	downstream := getEnv("DOWNSTREAM_URL", "http://orders:8080")
	interval := getEnvDuration("REQUEST_INTERVAL", 300*time.Millisecond)
	jitter := interval / 2

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	go http.ListenAndServe(":"+port, mux)

	client := &http.Client{Timeout: 5 * time.Second}
	log.Printf("gateway: self-driving traffic to %s every ~%s", downstream, interval)

	for {
		sleep := interval + time.Duration(rand.Int63n(int64(jitter)))
		time.Sleep(sleep)

		traceID := uuid.NewString()
		path := paths[rand.Intn(len(paths))]
		method := methods[rand.Intn(len(methods))]
		start := time.Now()

		logJSON(map[string]any{
			"level":    "info",
			"msg":      "incoming request",
			"trace_id": traceID,
			"service":  "gateway",
			"method":   method,
			"path":     path,
		})

		req, _ := http.NewRequest("GET", downstream+path, nil)
		req.Header.Set("X-Trace-Id", traceID)
		resp, err := client.Do(req)
		status := 0
		if err != nil {
			logJSON(map[string]any{
				"level":    "error",
				"msg":      "downstream call failed",
				"trace_id": traceID,
				"service":  "gateway",
				"error":    err.Error(),
			})
		} else {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			status = resp.StatusCode
		}

		logJSON(map[string]any{
			"level":       levelForStatus(status),
			"msg":         "request completed",
			"trace_id":    traceID,
			"service":     "gateway",
			"method":      method,
			"path":        path,
			"status_code": status,
			"duration_ms": time.Since(start).Milliseconds(),
		})
	}
}

func runOrders() {
	port := getEnv("PORT", "8080")
	downstream := getEnv("DOWNSTREAM_URL", "http://payments:8080")
	callDownstreamProb := getEnvFloat("DOWNSTREAM_CALL_PROBABILITY", 0.65)
	errProb := getEnvFloat("ERROR_PROBABILITY", 0.05)
	slowProb := getEnvFloat("SLOW_PROBABILITY", 0.1)
	client := &http.Client{Timeout: 5 * time.Second}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		traceID := r.Header.Get("X-Trace-Id")
		if traceID == "" {
			traceID = uuid.NewString()
		}

		logJSON(map[string]any{
			"level":    "info",
			"msg":      "received request",
			"trace_id": traceID,
			"service":  "orders",
			"method":   r.Method,
			"path":     r.URL.Path,
		})

		if rand.Float64() < callDownstreamProb {
			req, _ := http.NewRequest("GET", downstream+"/", nil)
			req.Header.Set("X-Trace-Id", traceID)
			resp, err := client.Do(req)
			if err != nil {
				logJSON(map[string]any{
					"level":    "error",
					"msg":      "payments call failed",
					"trace_id": traceID,
					"service":  "orders",
					"error":    err.Error(),
				})
			} else {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}

		status, extraDelay := simulateOutcome(errProb, slowProb)
		if extraDelay > 0 {
			time.Sleep(extraDelay)
		}

		logJSON(map[string]any{
			"level":       levelForStatus(status),
			"msg":         "request completed",
			"trace_id":    traceID,
			"service":     "orders",
			"status_code": status,
			"duration_ms": time.Since(start).Milliseconds(),
		})

		w.WriteHeader(status)
	})

	log.Printf("orders listening on :%s, downstream=%s", port, downstream)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func runPayments() {
	port := getEnv("PORT", "8080")
	errProb := getEnvFloat("ERROR_PROBABILITY", 0.05)
	slowProb := getEnvFloat("SLOW_PROBABILITY", 0.15)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		traceID := r.Header.Get("X-Trace-Id")
		if traceID == "" {
			traceID = uuid.NewString()
		}

		logJSON(map[string]any{
			"level":    "info",
			"msg":      "received request",
			"trace_id": traceID,
			"service":  "payments",
			"method":   r.Method,
			"path":     r.URL.Path,
		})

		status, extraDelay := simulateOutcome(errProb, slowProb)
		if extraDelay > 0 {
			time.Sleep(extraDelay)
		}

		logJSON(map[string]any{
			"level":       levelForStatus(status),
			"msg":         "request completed",
			"trace_id":    traceID,
			"service":     "payments",
			"status_code": status,
			"duration_ms": time.Since(start).Milliseconds(),
		})

		w.WriteHeader(status)
	})

	log.Printf("payments listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func levelForStatus(status int) string {
	if status >= 500 {
		return "error"
	}
	if status >= 400 {
		return "warn"
	}
	return "info"
}
