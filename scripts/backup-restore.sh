#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   ./scripts/backup-restore.sh backup /path/chatapp.sql
#   ./scripts/backup-restore.sh verify /path/chatapp.sql
#   ./scripts/backup-restore.sh restore /path/chatapp.sql
# The script never silently overwrites a database and requires explicit
# RESTORE_CONFIRM=YES for destructive restore operations.

: "${DATABASE_URL:=postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable}"
command -v pg_dump >/dev/null || { echo "pg_dump is required" >&2; exit 127; }
command -v pg_restore >/dev/null || { echo "pg_restore is required" >&2; exit 127; }

mode="${1:-}"
file="${2:-}"
[[ -n "$mode" && -n "$file" ]] || { echo "usage: $0 backup|verify|restore FILE" >&2; exit 2; }

case "$mode" in
  backup)
    umask 077
    tmp="${file}.tmp"
    trap 'rm -f "$tmp"' EXIT
    pg_dump --dbname="$DATABASE_URL" --format=custom --no-owner --no-privileges --file="$tmp"
    pg_restore --list "$tmp" >/dev/null
    mv -f "$tmp" "$file"
    echo "backup verified: $file"
    ;;
  verify)
    pg_restore --list "$file" >/dev/null
    echo "backup archive is readable: $file"
    ;;
  restore)
    [[ "${RESTORE_CONFIRM:-}" == "YES" ]] || { echo "set RESTORE_CONFIRM=YES for restore" >&2; exit 3; }
    pg_restore --dbname="$DATABASE_URL" --clean --if-exists --no-owner --no-privileges "$file"
    echo "restore completed: $file"
    ;;
  *) echo "unknown mode: $mode" >&2; exit 2 ;;
esac
