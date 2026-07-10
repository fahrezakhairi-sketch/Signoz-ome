#!/usr/bin/env bash
# Generator traffic sederhana untuk mendemonstrasikan golden signal RPS di SigNoz.
#
# Usage:
#   ./load-generator.sh [requests_per_second] [duration_seconds]
#
# Contoh: kirim ~10 req/detik selama 3 menit
#   ./load-generator.sh 10 180

RPS="${1:-5}"
DURATION="${2:-120}"
URL="${CHECKOUT_URL:-http://localhost:8081/checkout}"

END=$((SECONDS + DURATION))
echo "Sending ~${RPS} req/s to ${URL} for ${DURATION}s..."

while [ $SECONDS -lt $END ]; do
  for _ in $(seq 1 "$RPS"); do
    curl -s -o /dev/null "$URL" &
  done
  sleep 1
done
wait
echo "Done."
