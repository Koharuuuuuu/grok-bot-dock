#!/usr/bin/env bash
set -euo pipefail

is_runtime_input() {
  case "$1" in
    cmd/*|internal/*|frontend/*|go.mod|go.sum|Dockerfile|.dockerignore|Makefile|scripts/release-paths.sh|.github/workflows/ci.yml)
      return 0
      ;;
    *) return 1 ;;
  esac
}

if [[ "${1:-}" == "--test" ]]; then
  for path in cmd/server/main.go internal/api/server.go frontend/src/App.vue go.mod go.sum Dockerfile .dockerignore Makefile scripts/release-paths.sh .github/workflows/ci.yml; do
    is_runtime_input "$path" || { echo "expected image-affecting path: $path" >&2; exit 1; }
  done
  for path in README.md docs/TECHNICAL_DESIGN.md docs/OPTIMIZATION_ACCEPTANCE.md output/playwright/p5-mobile-320.png; do
    if is_runtime_input "$path"; then echo "unexpected release for documentation/artifact path: $path" >&2; exit 1; fi
  done
  echo "release path filter: passed"
  exit 0
fi

while IFS= read -r path; do
  if is_runtime_input "$path"; then echo true; exit 0; fi
done
echo false
