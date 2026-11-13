#!/bin/sh
set -eu
umask 077
cp /run/secrets/erlang-cookie /var/lib/rabbitmq/.erlang.cookie
chown rabbitmq:rabbitmq /var/lib/rabbitmq/.erlang.cookie
chmod 400 /var/lib/rabbitmq/.erlang.cookie
exec /usr/local/bin/docker-entrypoint.sh "$@"
