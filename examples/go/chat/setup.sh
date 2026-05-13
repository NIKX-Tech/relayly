#!/usr/bin/env bash
# setup.sh — register two chat devices in the local Relayly server.
#
# Run this once after `docker compose up --build -d`.
# It will print the --device-id and --token values for both devices.
#
# Usage:
#   ./setup.sh
#   ./setup.sh relayly   # custom container name

set -euo pipefail

CONTAINER="${1:-relayly}"

echo "📋  Registering devices in container '${CONTAINER}' …"
echo

# ── Register Device A ────────────────────────────────────────────────────────
OUTPUT_A=$(docker exec "${CONTAINER}" /relayly pair chat-device-a --no-qr 2>&1)
DEVICE_ID_A=$(echo "${OUTPUT_A}" | grep -E '^\s+ID:' | awk '{print $2}')
TOKEN_A=$(echo "${OUTPUT_A}"    | grep -E '^\s+Token:' | awk '{print $2}')

# ── Register Device B ────────────────────────────────────────────────────────
OUTPUT_B=$(docker exec "${CONTAINER}" /relayly pair chat-device-b --no-qr 2>&1)
DEVICE_ID_B=$(echo "${OUTPUT_B}" | grep -E '^\s+ID:' | awk '{print $2}')
TOKEN_B=$(echo "${OUTPUT_B}"    | grep -E '^\s+Token:' | awk '{print $2}')

# ── Link the two devices ─────────────────────────────────────────────────────
docker exec "${CONTAINER}" /relayly link "${DEVICE_ID_A}" "${DEVICE_ID_B}" > /dev/null

echo "✅  Done! Run these two commands in separate terminals:"
echo
echo "  Terminal 1 (Device A):"
echo "    cd examples/go/chat && go run . --role=a --device-id=${DEVICE_ID_A} --token=${TOKEN_A}"
echo
echo "  Terminal 2 (Device B):"
echo "    cd examples/go/chat && go run . --role=b --device-id=${DEVICE_ID_B} --token=${TOKEN_B}"
echo
