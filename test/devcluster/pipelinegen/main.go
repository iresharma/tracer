// Command pipelinegen continuously simulates CI/CD-style pipeline runs for
// exercising the tracer dev cluster with a trace "shape" that's different
// from httpgen's cross-service HTTP calls: here, one trace_id (the pipeline
// run id) covers many sequential stage log lines emitted by a single pod,
// with occasional stage failures that abort the remaining stages. No
// external trigger is needed — it drives itself in an infinite loop.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type pipeline struct {
	name   string
	stages []string
}

var pipelines = []pipeline{
	{"frontend-build", []string{"checkout", "install_deps", "lint", "unit_test", "build", "package"}},
	{"backend-build", []string{"checkout", "install_deps", "vet", "unit_test", "integration_test", "build", "package"}},
	{"deploy-prod", []string{"checkout", "build", "package", "push_image", "deploy", "smoke_test"}},
	{"nightly-e2e", []string{"checkout", "provision_env", "seed_data", "e2e_test", "teardown_env"}},
}

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

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
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

func main() {
	port := getEnv("PORT", "8080")
	runInterval := getEnvDuration("RUN_INTERVAL", 2*time.Second)
	stageMaxDuration := getEnvDuration("STAGE_MAX_DURATION", 400*time.Millisecond)
	stageFailProb := getEnvFloat("STAGE_FAIL_PROBABILITY", 0.08)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	go http.ListenAndServe(":"+port, mux)

	log.Printf("pipelinegen: running pipelines every ~%s, stages up to %s, fail chance %.0f%%", runInterval, stageMaxDuration, stageFailProb*100)

	for {
		runPipeline(stageMaxDuration, stageFailProb)
		jitter := runInterval / 2
		time.Sleep(runInterval + time.Duration(rand.Int63n(int64(jitter)+1)))
	}
}

func runPipeline(stageMaxDuration time.Duration, stageFailProb float64) {
	p := pipelines[rand.Intn(len(pipelines))]
	runID := uuid.NewString()
	pipelineStart := time.Now()

	logJSON(map[string]any{
		"level":    "info",
		"msg":      "pipeline started",
		"event":    "pipeline_start",
		"trace_id": runID,
		"pipeline": p.name,
		"stages":   len(p.stages),
	})

	failedAt := ""
	for _, stage := range p.stages {
		stageStart := time.Now()
		logJSON(map[string]any{
			"level":    "info",
			"msg":      "stage started",
			"event":    "stage_start",
			"trace_id": runID,
			"pipeline": p.name,
			"stage":    stage,
		})

		time.Sleep(time.Duration(rand.Int63n(int64(stageMaxDuration))) + 20*time.Millisecond)

		failed := rand.Float64() < stageFailProb
		status := "success"
		level := "info"
		if failed {
			status = "failed"
			level = "error"
		}

		logJSON(map[string]any{
			"level":       level,
			"msg":         "stage finished",
			"event":       "stage_end",
			"trace_id":    runID,
			"pipeline":    p.name,
			"stage":       stage,
			"status":      status,
			"duration_ms": time.Since(stageStart).Milliseconds(),
		})

		if failed {
			failedAt = stage
			break
		}
	}

	status := "success"
	level := "info"
	if failedAt != "" {
		status = "failed"
		level = "error"
	}
	logJSON(map[string]any{
		"level":        level,
		"msg":          "pipeline finished",
		"event":        "pipeline_end",
		"trace_id":     runID,
		"pipeline":     p.name,
		"status":       status,
		"failed_stage": failedAt,
		"duration_ms":  time.Since(pipelineStart).Milliseconds(),
	})
}
