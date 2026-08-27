#!/bin/sh
set -eu

export OPENDEPOT_SERVER_HOST="${OPENDEPOT_SERVER_HOST:-server.opendepot-system.svc.cluster.local:80}"

envsubst "\${OPENDEPOT_SERVER_HOST}" < /etc/nginx/nginx.conf.template > /tmp/nginx.conf

nginx -c /tmp/nginx.conf -g "daemon off;" &
nginx_pid=$!

HOSTNAME=127.0.0.1 /app/node_modules/.bin/next dev --hostname 127.0.0.1 --port 3000 &
next_pid=$!

trap 'kill -TERM "$nginx_pid" "$next_pid" 2>/dev/null || true; wait "$nginx_pid" "$next_pid" 2>/dev/null || true' INT TERM EXIT

while kill -0 "$nginx_pid" 2>/dev/null && kill -0 "$next_pid" 2>/dev/null; do
  sleep 1
done

exit 1
