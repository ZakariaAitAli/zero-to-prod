#!/bin/sh
# Install the PostgreSQL server key and certificate (ADR 0006).
#
# The key and certificate arrive as one file in a read-only directory mount
# (/run/internal-tls/tls.pem), owned by the host user. PostgreSQL refuses a
# key it does not own with mode 0600 (or root's with 0640), so this copies
# the file into a tmpfs owned by postgres, replacing it by rename.
#
# Without arguments (renewal): install only; then reload PostgreSQL
# (pg_ctl reload). New connections use the new certificate; established
# connections keep their session.
# With arguments (container entrypoint): install, then run the image's
# entrypoint with them.
set -eu

source_file=/run/internal-tls/tls.pem
target_dir=/run/postgresql-tls

if [ ! -f "$source_file" ] || [ ! -s "$source_file" ]; then
  echo "error: PostgreSQL TLS file missing or empty: ${source_file}" >&2
  exit 1
fi

install -d -o postgres -g postgres -m 700 "$target_dir"
install -o postgres -g postgres -m 600 "$source_file" "${target_dir}/server.pem.new"
mv -f "${target_dir}/server.pem.new" "${target_dir}/server.pem"

if [ "$#" -gt 0 ]; then
  exec docker-entrypoint.sh "$@"
fi
