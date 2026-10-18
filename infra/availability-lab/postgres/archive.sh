#!/bin/sh
set -eu
source="$1"; name="$2"
case "$name" in ''|.|..|*[!0-9A-Fa-f.historybackup]*) exit 1;; esac
# Refuse overwriting different content, including across timeline changes.
if [ -f "/wal-archive/$name" ]; then cmp -s "$source" "/wal-archive/$name"; exit $?; fi
cp "$source" "/wal-archive/$name.partial.$$"
sync
mv "/wal-archive/$name.partial.$$" "/wal-archive/$name"
sync
