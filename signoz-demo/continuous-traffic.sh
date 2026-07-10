#!/usr/bin/env bash
# Continuous traffic generator untuk demo Signoz (checkout-service).
# Berjalan terus-menerus dengan RPS bervariasi (bukan flat) sampai di-stop manual.
#
# Usage:
#   ./continuous-traffic.sh                 # jalan foreground, RPS random 5-20
#   ./continuous-traffic.sh --auto-chaos    # tambahan: otomatis siklus latency/error tiap beberapa menit
#
# Stop dengan Ctrl+C.

CHECKOUT_URL="${CHECKOUT_URL:-http://localhost:8081/checkout}"
PAYMENT_CONFIG_URL="${PAYMENT_CONFIG_URL:-http://localhost:8082/config}"
MIN_RPS=5
MAX_RPS=20
AUTO_CHAOS=false

if [ "$1" == "--auto-chaos" ]; then
  AUTO_CHAOS=true
fi

echo "Sending continuous traffic to ${CHECKOUT_URL}"
echo "RPS range: ${MIN_RPS}-${MAX_RPS} req/s (random tiap detik)"
[ "$AUTO_CHAOS" == "true" ] && echo "Auto-chaos: ON (siklus latency/error tiap 90 detik)"
echo "Tekan Ctrl+C untuk stop."
echo ""

cleanup() {
  echo ""
  echo "Stopping... reset payment-service ke kondisi normal."
  curl -s -X POST "${PAYMENT_CONFIG_URL}?latency_ms=0&error_rate=0" > /dev/null
  wait 2>/dev/null
  echo "Done."
  exit 0
}
trap cleanup INT TERM

# Jalankan siklus chaos di background kalau diaktifkan
run_auto_chaos() {
  while true; do
    echo "[chaos] baseline (latency=0, error=0)"
    curl -s -X POST "${PAYMENT_CONFIG_URL}?latency_ms=0&error_rate=0" > /dev/null
    sleep 90

    echo "[chaos] latency spike (latency_ms=800)"
    curl -s -X POST "${PAYMENT_CONFIG_URL}?latency_ms=800&error_rate=0" > /dev/null
    sleep 60

    echo "[chaos] error spike (error_rate=0.3)"
    curl -s -X POST "${PAYMENT_CONFIG_URL}?latency_ms=0&error_rate=0.3" > /dev/null
    sleep 60
  done
}

if [ "$AUTO_CHAOS" == "true" ]; then
  run_auto_chaos &
  CHAOS_PID=$!
fi

# Loop traffic utama — RPS random tiap detik biar grafik terlihat natural
while true; do
  RPS=$(( RANDOM % (MAX_RPS - MIN_RPS + 1) + MIN_RPS ))
  for _ in $(seq 1 "$RPS"); do
    curl -s -o /dev/null "$CHECKOUT_URL" &
  done
  sleep 1
done
