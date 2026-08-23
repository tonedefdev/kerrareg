#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)

mode=safe
dry_run=false

usage() {
  cat <<'EOF'
Usage: tilt/scripts/cleanup-images.sh [--all] [--dry-run]

Remove unused images created by Tilt for the OpenDepot development environment.

  --all      Recreate the local Kind cluster and registry before cleaning host images.
             Run this only after stopping Tilt and running `tilt down`.
  --dry-run  Show what would be removed without changing Docker resources.
EOF
}

for argument in "$@"; do
  case "$argument" in
    --all)
      mode=all
      ;;
    --dry-run)
      dry_run=true
      ;;
    -h|--help)
      usage

      exit 0
      ;;
    *)
      echo "Unknown argument: $argument" >&2
      usage >&2

      exit 2
      ;;
  esac
done

cd "$repo_root"

if ! command -v docker >/dev/null 2>&1; then
  echo "Missing required development tool: docker" >&2

  exit 1
fi

docker info >/dev/null

list_images() {
  docker image ls \
    --filter 'label=dev.tilt.gc=true' \
    --filter 'reference=localhost:5005/ghcr.io_tonedefdev_opendepot_*:tilt-*' \
    --format '{{.Repository}}:{{.Tag}}' | sort -u
}

list_active_kubernetes_images() {
  if ! command -v kubectl >/dev/null 2>&1; then
    return
  fi

  {
    kubectl --context kind-opendepot get pods -A \
      -o jsonpath='{range .items[*].spec.initContainers[*]}{.image}{"\n"}{end}{range .items[*].spec.containers[*]}{.image}{"\n"}{end}' \
      2>/dev/null || true
    kubectl --context kind-opendepot get deployments,statefulsets,daemonsets,jobs -A \
      -o jsonpath='{range .items[*].spec.template.spec.initContainers[*]}{.image}{"\n"}{end}{range .items[*].spec.template.spec.containers[*]}{.image}{"\n"}{end}' \
      2>/dev/null || true
    kubectl --context kind-opendepot get cronjobs -A \
      -o jsonpath='{range .items[*].spec.jobTemplate.spec.template.spec.initContainers[*]}{.image}{"\n"}{end}{range .items[*].spec.jobTemplate.spec.template.spec.containers[*]}{.image}{"\n"}{end}' \
      2>/dev/null || true
  } | sort -u
}

cleanup_host_images() {
  image_references=$(list_images)
  active_image_references=$(list_active_kubernetes_images)

  if [ -z "$image_references" ]; then
    echo "No OpenDepot Tilt images found in host Docker."

    return
  fi

  removed=0
  retained=0
  while IFS= read -r image_reference; do
    if [ -n "$active_image_references" ] && printf '%s\n' "$active_image_references" | grep -Fqx -- "$image_reference"; then
      retained=$((retained + 1))
      echo "Retained image used by a Kubernetes pod: $image_reference"
    elif [ "$dry_run" = true ]; then
      removed=$((removed + 1))
      echo "Would remove: $image_reference"
    elif docker image rm "$image_reference" >/dev/null 2>&1; then
      removed=$((removed + 1))
    else
      retained=$((retained + 1))
      echo "Retained image still in use: $image_reference"
    fi
  done <<EOF
$image_references
EOF

  if [ "$dry_run" = true ]; then
    echo "Would remove $removed OpenDepot Tilt image references; would retain $retained active references."
  else
    echo "Removed $removed OpenDepot Tilt image references; retained $retained active references."
  fi
}

list_kind_images() {
  if ! docker container inspect opendepot-control-plane >/dev/null 2>&1; then
    return
  fi

  docker exec opendepot-control-plane crictl images | awk '
    $1 ~ /^localhost:5005\/ghcr.io_tonedefdev_opendepot_/ && $2 ~ /^tilt-/ {
      print $1 ":" $2
    }
  ' | sort -u
}

cleanup_kind_images() {
  image_references=$(list_kind_images)
  active_image_references=$(list_active_kubernetes_images)

  if [ -z "$image_references" ]; then
    echo "No OpenDepot Tilt images found in the Kind node."

    return
  fi

  removed=0
  retained=0
  while IFS= read -r image_reference; do
    if [ -n "$active_image_references" ] && printf '%s\n' "$active_image_references" | grep -Fqx -- "$image_reference"; then
      retained=$((retained + 1))
      echo "Retained Kind image used by a Kubernetes pod: $image_reference"
    elif [ "$dry_run" = true ]; then
      removed=$((removed + 1))
      echo "Would remove from Kind: $image_reference"
    elif docker exec opendepot-control-plane crictl rmi "$image_reference" >/dev/null 2>&1; then
      removed=$((removed + 1))
    else
      retained=$((retained + 1))
      echo "Retained Kind image still in use: $image_reference"
    fi
  done <<EOF
$image_references
EOF

  if [ "$dry_run" = true ]; then
    echo "Would remove $removed OpenDepot Tilt image references from Kind; would retain $retained active references."
  else
    echo "Removed $removed OpenDepot Tilt image references from Kind; retained $retained active references."
  fi
}

require_full_cleanup_tools() {
  for command_name in ctlptl kubectl tilt; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      echo "Missing required development tool: $command_name" >&2

      exit 1
    fi
  done
}

ensure_environment_stopped() {
  if tilt get session >/dev/null 2>&1; then
    echo "Tilt is still running. Stop it before using --all." >&2

    exit 1
  fi

  workloads=$(kubectl --context kind-opendepot get all -n opendepot-system -o name 2>/dev/null || true)

  if [ -n "$workloads" ]; then
    echo "OpenDepot workloads are still deployed. Run 'tilt down' before using --all." >&2

    exit 1
  fi
}

recreate_local_environment() {
  if [ "$dry_run" = true ]; then
    echo "Would recreate the ctlptl cluster and registry from tilt/cluster.yaml."

    return
  fi

  echo "Recreating the OpenDepot Kind cluster and local registry..."
  DOCKER_API_VERSION="${DOCKER_API_VERSION:-1.41}" ctlptl delete -f tilt/cluster.yaml || true
  DOCKER_API_VERSION="${DOCKER_API_VERSION:-1.41}" ctlptl apply -f tilt/cluster.yaml
}

echo "Docker disk usage before cleanup:"
docker system df

if [ "$mode" = all ]; then
  require_full_cleanup_tools
  ensure_environment_stopped
  recreate_local_environment
else
  cleanup_kind_images
fi

cleanup_host_images

if [ "$dry_run" = false ]; then
  echo "Docker disk usage after cleanup:"
  docker system df
fi