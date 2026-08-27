#!/bin/sh
set -eu

namespace=opendepot-system
kubectl delete job trivy-cache-db --namespace "$namespace" --ignore-not-found
kubectl create job trivy-cache-db --from=cronjob/trivy-db-updater --namespace "$namespace"
kubectl wait --for=condition=complete job/trivy-cache-db --namespace "$namespace" --timeout=5m