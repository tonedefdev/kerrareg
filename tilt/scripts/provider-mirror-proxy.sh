#!/bin/sh
set -eu

if ! command -v mkcert >/dev/null 2>&1; then
  echo "mkcert is required to run the provider mirror TLS proxy" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required to run the provider mirror TLS proxy" >&2
  exit 1
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
certificate_dir=$(mktemp -d -t opendepot-mirror-tls)
proxy_pid=""

cleanup() {
  if [ -n "$proxy_pid" ]; then
    kill "$proxy_pid" 2>/dev/null || true
    wait "$proxy_pid" 2>/dev/null || true
  fi
  rm -rf "$certificate_dir"
}
trap cleanup EXIT INT TERM

certificate_path="$certificate_dir/mirror.crt"
private_key_path="$certificate_dir/mirror.key"

mkcert \
  -cert-file "$certificate_path" \
  -key-file "$private_key_path" \
  opendepot.localtest.me localhost 127.0.0.1 ::1

go run "$repo_root/tilt/scripts/provider-mirror-proxy.go" \
  --listen :8443 \
  --target http://127.0.0.1:8080 \
  --cert "$certificate_path" \
  --key "$private_key_path" &
proxy_pid=$!
wait "$proxy_pid"
