#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
tool="${ROOT_DIR}/tools/postgres-backup-local"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT

expect_failure() {
  local name="$1"
  shift
  if "$@" > "$test_dir/log" 2>&1; then
    echo "FAIL: $name unexpectedly passed" >&2
    exit 1
  fi
  echo "PASS: $name"
}

if [ "${1:-}" = --integration ]; then
  # Uses the selected Compose lab only for pg_dump and non-restoring reads.
  # No SQL from the archive is executed, and no successful recovery is claimed.
  "$tool" create "$test_dir/valid.dump"
  "$tool" validate "$test_dir/valid.dump"
  archive_bytes="$(wc -c < "$test_dir/valid.dump")"
  head -c "$((archive_bytes - 20))" "$test_dir/valid.dump" > "$test_dir/truncated.dump"
  # Establish that this failure is in payload readability, not TOC parsing.
  "$tool" inspect "$test_dir/truncated.dump" > "$test_dir/toc"
  expect_failure "real archive with readable TOC but truncated payload" \
    "$tool" validate "$test_dir/truncated.dump"
  grep -q 'payload is not readable' "$test_dir/log"
  exit 0
fi

mkdir "$test_dir/bin"
printf 'archive fixture\n' > "$test_dir/archive.dump"
cat > "$test_dir/toc" <<'TOC'
1; 0 0 TABLE DATA public work_items owner
2; 0 0 TABLE DATA public processing_jobs owner
3; 0 0 TABLE DATA public outbox_messages owner
4; 0 0 SEQUENCE SET public work_items_id_seq owner
5; 0 0 SEQUENCE SET public processing_jobs_id_seq owner
6; 0 0 SEQUENCE SET public outbox_messages_id_seq owner
TOC
cat > "$test_dir/bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$CALL_LOG"
case "$*" in
  'compose version') exit 0 ;;
  *'pg_restore --list') cat "$TOC_FILE"; exit "$TOC_RC" ;;
  *'pg_restore --file=/dev/null') cat >/dev/null; exit "$PAYLOAD_RC" ;;
  *) echo "unexpected command: $*" >&2; exit 99 ;;
esac
SH
chmod +x "$test_dir/bin/docker"
export PATH="$test_dir/bin:$PATH" CALL_LOG="$test_dir/calls"
export TOC_FILE="$test_dir/toc" TOC_RC=0 PAYLOAD_RC=0

"$tool" validate "$test_dir/archive.dump" > "$test_dir/log"
grep -q '^valid=' "$test_dir/log"
grep -q 'pg_restore --file=/dev/null' "$CALL_LOG"
echo 'PASS: scope and payload readability both checked'

PAYLOAD_RC=1
: > "$CALL_LOG"
expect_failure 'readable TOC but unreadable payload' "$tool" validate "$test_dir/archive.dump"
grep -q 'payload is not readable' "$test_dir/log"
expect_failure 'restore stops before target checks on unreadable payload' "$tool" restore "$test_dir/archive.dump"
if grep -q 'psql\|--single-transaction' "$CALL_LOG"; then
  echo 'FAIL: unreadable payload reached the restore target' >&2
  exit 1
fi

PAYLOAD_RC=0 TOC_RC=1
expect_failure 'unreadable TOC' "$tool" validate "$test_dir/archive.dump"
TOC_RC=0
cp "$test_dir/toc" "$test_dir/extra-toc"
printf '7; 0 0 TABLE DATA public unrelated owner\n' >> "$test_dir/extra-toc"
TOC_FILE="$test_dir/extra-toc"
expect_failure 'unexpected extra TOC entry' "$tool" validate "$test_dir/archive.dump"
sed 's/TABLE DATA public outbox_messages /TABLE DATA public unrelated /' "$test_dir/toc" > "$test_dir/wrong-toc"
TOC_FILE="$test_dir/wrong-toc"
: > "$CALL_LOG"
expect_failure 'six entries with wrong scope' "$tool" validate "$test_dir/archive.dump"
if grep -q -- '--file=/dev/null' "$CALL_LOG"; then
  echo 'FAIL: wrong scope reached payload validation' >&2
  exit 1
fi
expect_failure 'missing archive' "$tool" validate "$test_dir/missing.dump"
: > "$test_dir/empty.dump"
expect_failure 'empty archive' "$tool" validate "$test_dir/empty.dump"
