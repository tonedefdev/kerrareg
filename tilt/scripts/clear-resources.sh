#!/bin/sh
set -eu

kubectl delete module terraform-aws-key-pair --namespace opendepot-system --ignore-not-found
kubectl delete groupbinding local-test-access --namespace opendepot-system --ignore-not-found