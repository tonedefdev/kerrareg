#!/bin/sh
set -eu

DOCKER_API_VERSION="${DOCKER_API_VERSION:-1.41}" ctlptl delete -f tilt/cluster.yaml || true
rm -rf tilt/.generated
DOCKER_API_VERSION="${DOCKER_API_VERSION:-1.41}" ctlptl apply -f tilt/cluster.yaml