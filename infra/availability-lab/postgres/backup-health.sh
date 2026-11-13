#!/bin/sh
set -eu
last="$(cat /backups/last-verified)"
interval="${BACKUP_INTERVAL_SECONDS:-300}"
case "$last:$interval" in *[!0-9:]*) exit 1;; esac
[ "$interval" -gt 0 ]
[ "$(( $(date +%s) - last ))" -le "$(( interval * 3 ))" ]
