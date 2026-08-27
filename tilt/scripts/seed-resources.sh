#!/bin/sh
set -eu

kubectl apply -f - <<'EOF'
apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: terraform-aws-key-pair
  namespace: opendepot-system
spec:
  moduleConfig:
    fileFormat: zip
    githubClientConfig:
      useAuthenticatedClient: false
    provider: aws
    repoOwner: terraform-aws-modules
    repoUrl: https://github.com/terraform-aws-modules/terraform-aws-key-pair
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  versions:
    - version: v2.0.3
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: local-test-access
  namespace: opendepot-system
spec:
  expression: '"local-test-group" in groups'
  moduleResources:
    - terraform-aws-key-pair
EOF