#!/bin/sh
set -eu

namespace=opendepot-system
anonymous_auth=${OPENDEPOT_DEV_ANONYMOUS_AUTH:-false}

if [ -z "${OPENDEPOT_DEV_PASSWORD:-}" ]; then
  echo "OPENDEPOT_DEV_PASSWORD is required for the local Dex user" >&2
  exit 1
fi

case "$anonymous_auth" in
  true|false) ;;
  *)
    echo "OPENDEPOT_DEV_ANONYMOUS_AUTH must be true or false" >&2
    exit 1
    ;;
esac

if command -v htpasswd >/dev/null 2>&1; then
  password_hash=$(htpasswd -bnBC 10 "" "$OPENDEPOT_DEV_PASSWORD" | tr -d ':\n')
else
  password_hash=$(python3 -c 'import bcrypt, os; print(bcrypt.hashpw(os.environ["OPENDEPOT_DEV_PASSWORD"].encode(), bcrypt.gensalt(10)).decode())')
fi

kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

if ! kubectl get secret ui-session-secret --namespace "$namespace" >/dev/null 2>&1; then
  session_password=$(openssl rand -base64 48)
  kubectl create secret generic ui-session-secret \
    --from-literal=sessionPassword="$session_password" \
    --namespace "$namespace" >/dev/null
fi

kubectl create secret generic ui-oidc-secret \
  --from-literal=clientSecret=ui-local-test-secret \
  --namespace "$namespace" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

if ! kubectl get secret opendepot-provider-gpg --namespace "$namespace" >/dev/null 2>&1; then
  gpg_home=$(mktemp -d -t opendepot-gpg)
  trap 'rm -rf "$gpg_home"' EXIT

  cat > "$gpg_home/keygen.conf" <<'EOF'
%no-protection
Key-Type: RSA
Key-Length: 2048
Name-Real: OpenDepot Local
Name-Email: opendepot@local.test
Expire-Date: 0
%commit
EOF

  GNUPGHOME="$gpg_home" gpg --batch --gen-key "$gpg_home/keygen.conf" >/dev/null 2>&1
  key_id=$(GNUPGHOME="$gpg_home" gpg --list-keys --with-colons | awk -F: '/^fpr/{print $10; exit}')
  ascii_armor=$(GNUPGHOME="$gpg_home" gpg --armor --export "$key_id")
  private_key=$(GNUPGHOME="$gpg_home" gpg --armor --export-secret-keys "$key_id" | base64 | tr -d '\n')

  kubectl create secret generic opendepot-provider-gpg \
    --from-literal=OPENDEPOT_PROVIDER_GPG_KEY_ID="$key_id" \
    --from-literal=OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR="$ascii_armor" \
    --from-literal=OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64="$private_key" \
    --namespace "$namespace"
fi

mkdir -p tilt/.generated
cat > tilt/.generated/values.yaml <<EOF
server:
  anonymousAuth: $anonymous_auth

dex:
  config:
    staticPasswords:
      - email: dev@example.com
        hash: '$password_hash'
        username: devuser
        userID: local-test-user
        groups:
          - local-test-group
EOF