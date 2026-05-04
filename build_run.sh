#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "Running build..."
bash "$SCRIPT_DIR/build.sh"

echo "Starting backend..."
exec "$SCRIPT_DIR/backend/lumi" "$@"
