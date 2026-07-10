package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// chaosConfig menyimpan pengaturan simulasi yang bisa diubah live via /config
// tanpa restart — berguna saat demo screencast untuk menunjukkan tiap golden signal.
type chaosConfig struct {
	mu        sync.RWMutex
	latencyMs int     // tambahan latency buatan (ms) → demo Latency
	errorRate float64 // probabilitas error 0.0–1.0 → demo Error Rate
}

var (
	chaos  = &chaosConfig{}
	tracer = otel.Tracer("payment-service")
)

func (c *chaosConfig) get() (int, float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latencyMs, c.errorRate
}

func (c *chaosConfig) set(latencyMs int, errorRate float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latencyMs = latencyMs
	c.errorRate = errorRate
}

func main() {
	ctx := context.Background()

	shutdown, err := setupOTel(ctx, "payment-service")
	if err != nil {
		log.Fatalf("otel init: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()

	mux := http.NewServeMux()
	mux.Handle("/process", otelhttp.NewHandler(http.HandlerFunc(handleProcess), "GET /process"))
	// /config: ubah latency & error rate secara live
	// Contoh: curl -X POST "http://localhost:8082/config?latency_ms=800&error_rate=0.3"
	mux.HandleFunc("/config", handleConfig)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	port := getenv("PORT", "8082")
	server := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		log.Printf("payment-service listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func handleProcess(w http.ResponseWriter, r *http.Request) {
	// Span utama — muncul sebagai child dari span checkout-service di flame graph
	ctx, span := tracer.Start(r.Context(), "payment.process")
	defer span.End()

	latencyMs, errorRate := chaos.get()

	// Child span: simulasi DB query agar trace punya kedalaman
	// Ini yang ditunjukkan saat menjelaskan distributed tracing di screencast
	_, dbSpan := tracer.Start(ctx, "db.query", trace.WithAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.statement", "SELECT * FROM payments WHERE id = $1"),
	))
	time.Sleep(time.Duration(10+rand.Intn(20)) * time.Millisecond)
	dbSpan.End()

	// Latency buatan → naikkan latency_ms untuk demo golden signal Latency (p50/p95/p99)
	if latencyMs > 0 {
		time.Sleep(time.Duration(latencyMs) * time.Millisecond)
	}

	// Error injection → naikkan error_rate untuk demo golden signal Error Rate
	if rand.Float64() < errorRate {
		err := fmt.Errorf("simulated downstream payment failure")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "payment processed")
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	latencyMs, _ := strconv.Atoi(q.Get("latency_ms"))
	errorRate, _ := strconv.ParseFloat(q.Get("error_rate"), 64)
	chaos.set(latencyMs, errorRate)
	log.Printf("config updated: latency_ms=%d error_rate=%.2f", latencyMs, errorRate)
	fmt.Fprintf(w, "config updated: latency_ms=%d error_rate=%.2f\n", latencyMs, errorRate)
}

// setupOTel menginisialisasi OpenTelemetry tracer dengan OTLP HTTP exporter ke SigNoz.
func setupOTel(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	endpoint := getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")

	traceURL := strings.TrimRight(endpoint, "/") + "/v1/traces"

	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(traceURL))
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.DeploymentEnvironment(getenv("OTEL_DEPLOYMENT_ENVIRONMENT", "demo")),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Re-ambil tracer setelah provider di-set agar menggunakan provider yang benar
	tracer = otel.Tracer("payment-service")

	log.Printf("OTel → SigNoz: %s  service=%s", traceURL, serviceName)
	return tp.Shutdown, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
