#!/usr/bin/env bash
# Load generator multi-endpoint untuk demo golden signals di Signoz.
# Mengirim traffic campuran ke 4 endpoint checkout-service secara terus-menerus
# dengan RPS yang bervariasi (bukan flat), sampai di-stop manual (Ctrl+C).
#
#   /ping  -> baseline normal traffic (demo RPS)
#   /slow  -> random delay 500-1500ms (demo Latency p50/p95/p99)
#   /error -> selalu HTTP 500 (demo Error Rate)
#   /echo  -> POST dengan body (demo variasi traffic + tracing)
#
# Usage:
#   ./load-generator-multi.sh                 # jalan terus, total RPS random 10-30
#   ./load-generator-multi.sh 20              # jalan terus, target total RPS ~20 (+/- variasi)
#   ./load-generator-multi.sh 20 180          # target RPS ~20, berhenti otomatis setelah 180 detik
#
# Stop dengan Ctrl+C (kalau tidak diberi durasi, script jalan tanpa batas).

BASE_URL="${CHECKOUT_URL_BASE:-http://localhost:8081}"
TARGET_RPS="${1:-0}"        # 0 = random 10-30 tiap detik
DURATION="${2:-0}"          # 0 = jalan tanpa batas waktu

# Proporsi traffic per endpoint (dari total RPS)
PING_PCT=60
SLOW_PCT=15
ERROR_PCT=10
ECHO_PCT=15

echo "Sending continuous mixed traffic to ${BASE_URL}"
if [ "$TARGET_RPS" -eq 0 ]; then
  echo "Total RPS: random 10-30 req/s (berubah tiap detik)"
else
  echo "Total RPS: ~${TARGET_RPS} req/s"
fi
if [ "$DURATION" -eq 0 ]; then
  echo "Durasi: tanpa batas — tekan Ctrl+C untuk stop"
else
  echo "Durasi: ${DURATION} detik"
fi
echo ""

cleanup() {
  echo ""
  echo "Stopping traffic generator..."
  wait 2>/dev/null
  echo "Done."
  exit 0
}
trap cleanup INT TERM

START=$SECONDS
while true; do
  if [ "$DURATION" -ne 0 ] && [ $((SECONDS - START)) -ge "$DURATION" ]; then
    cleanup
  fi

  if [ "$TARGET_RPS" -eq 0 ]; then
    RPS=$(( RANDOM % 21 + 10 ))   # random 10-30
  else
    RPS=$TARGET_RPS
  fi

  PING_RPS=$(( RPS * PING_PCT / 100 ))
  SLOW_RPS=$(( RPS * SLOW_PCT / 100 ))
  ERROR_RPS=$(( RPS * ERROR_PCT / 100 ))
  ECHO_RPS=$(( RPS * ECHO_PCT / 100 ))

  for _ in $(seq 1 "$PING_RPS"); do
    curl -s -o /dev/null "${BASE_URL}/ping" &
  done
  for _ in $(seq 1 "$SLOW_RPS"); do
    curl -s -o /dev/null "${BASE_URL}/slow" &
  done
  for _ in $(seq 1 "$ERROR_RPS"); do
    curl -s -o /dev/null "${BASE_URL}/error" &
  done
  for _ in $(seq 1 "$ECHO_RPS"); do
    curl -s -o /dev/null -X POST -d '{"msg":"hello"}' -H "Content-Type: application/json" "${BASE_URL}/echo" &
  done

  sleep 1
done
