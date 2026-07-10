package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// client membungkus transport OTel agar trace context otomatis diteruskan
// ke payment-service lewat HTTP header — inilah yang membuat tracing "distributed".
var client = &http.Client{
	Transport: otelhttp.NewTransport(http.DefaultTransport),
}

func main() {
	ctx := context.Background()

	shutdown, err := setupOTel(ctx, "checkout-service")
	if err != nil {
		log.Fatalf("otel init: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()

	mux := http.NewServeMux()

	// Endpoint lama — checkout memanggil payment-service (demo distributed tracing)
	mux.Handle("/checkout", otelhttp.NewHandler(http.HandlerFunc(handleCheckout), "GET /checkout"))

	// Endpoint baru — masing-masing fokus mendemonstrasikan 1 golden signal
	mux.Handle("/ping", otelhttp.NewHandler(http.HandlerFunc(handlePing), "GET /ping"))
	mux.Handle("/slow", otelhttp.NewHandler(http.HandlerFunc(handleSlow), "GET /slow"))
	mux.Handle("/error", otelhttp.NewHandler(http.HandlerFunc(handleError), "GET /error"))
	mux.Handle("/echo", otelhttp.NewHandler(http.HandlerFunc(handleEcho), "POST /echo"))

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	port := getenv("PORT", "8081")
	server := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		log.Printf("checkout-service listening on :%s", port)
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

// handleCheckout: entry point lama, memanggil payment-service (distributed tracing demo)
func handleCheckout(w http.ResponseWriter, r *http.Request) {
	paymentURL := getenv("PAYMENT_SERVICE_URL", "http://localhost:8082") + "/process"

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, paymentURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// handlePing: baseline normal traffic, demo golden signal RPS
func handlePing(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "pong")
}

// handleSlow: random delay, demo golden signal Latency (p50/p95/p99)
func handleSlow(w http.ResponseWriter, r *http.Request) {
	delayMs := 500 + rand.Intn(1000) // 500-1500ms
	time.Sleep(time.Duration(delayMs) * time.Millisecond)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "slow response after %dms\n", delayMs)
}

// handleError: selalu gagal, demo golden signal Error Rate
func handleError(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "simulated error", http.StatusInternalServerError)
}

// handleEcho: POST dengan body, variasi traffic + demo tracing dengan payload
func handleEcho(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	w.WriteHeader(http.StatusOK)
	w.Write(body)
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

	log.Printf("OTel -> SigNoz: %s  service=%s", traceURL, serviceName)
	return tp.Shutdown, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
