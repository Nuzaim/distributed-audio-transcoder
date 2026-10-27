#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Usage help
show_help() {
  cat << EOF
Distributed Audio Transcoder Test Runner

Usage:
  $(basename "$0") [OPTIONS] [-- PYTHON_TEST_OPTIONS]

Options:
  --up, -u            Start the Docker Compose development stack before testing
  --down, -d          Shut down the Docker Compose stack after testing
  --go, -g            Run Go transcoder unit and integration tests only
  --generate          Generate sample test audio files in test_data/
  --check             Check service connectivity (API, MinIO, ElasticMQ)
  --help, -h          Show this help message

Examples:
  ./scripts/test_app.sh                       # Run E2E test suite (services must be running)
  ./scripts/test_app.sh --up                  # Start Docker Compose stack and run tests
  ./scripts/test_app.sh --go                  # Test Go transcoder worker on test audio
  ./scripts/test_app.sh --file test_data/sample_5s.mp3
  ./scripts/test_app.sh --concurrent 4        # Test concurrent uploads against 4 workers
EOF
}

START_STACK=false
STOP_STACK=false
FORWARD_ARGS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --up|-u)
      START_STACK=true
      shift
      ;;
    --down|-d)
      STOP_STACK=true
      shift
      ;;
    --go|-g)
      FORWARD_ARGS+=("--go-tests")
      shift
      ;;
    --help|-h)
      show_help
      exit 0
      ;;
    *)
      FORWARD_ARGS+=("$1")
      shift
      ;;
  esac
done

cleanup() {
  if [ "$STOP_STACK" = true ]; then
    echo "Stopping Docker Compose stack..."
    docker compose -f "${REPO_ROOT}/deploy/dev/docker-compose.yml" down
  fi
}
trap cleanup EXIT

if [ "$START_STACK" = true ]; then
  echo "Starting development stack with Docker Compose..."
  mkdir -p "${REPO_ROOT}/runtime/uploads" "${REPO_ROOT}/runtime/tmp" "${REPO_ROOT}/runtime/hls"
  docker compose -f "${REPO_ROOT}/deploy/dev/docker-compose.yml" up -d --build
  echo "Waiting 5 seconds for services to initialize..."
  sleep 5
fi

# Run python test runner
python3 "${SCRIPT_DIR}/test_app.py" "${FORWARD_ARGS[@]}"
