#!/bin/sh
set -eu

missing=""

for command_name in docker kubectl helm ctlptl gpg openssl tilt; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    missing="$missing $command_name"
  fi
done

if [ -n "$missing" ]; then
  echo "Missing required development tools:$missing" >&2
  exit 1
fi

docker info >/dev/null
DOCKER_API_VERSION="${DOCKER_API_VERSION:-1.41}" ctlptl apply -f tilt/cluster.yaml

current_context=$(kubectl config current-context)
if [ "$current_context" != "kind-opendepot" ]; then
  echo "Expected Kubernetes context kind-opendepot, got $current_context" >&2
  exit 1
fi