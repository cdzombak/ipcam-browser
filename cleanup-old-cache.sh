#!/usr/bin/env bash
set -euo pipefail

# cleanup-old-cache.sh
# Removes cached media files older than 30 days from ipcam-browser cache directories
#
# Usage:
#   ./cleanup-old-cache.sh [cache-root-dir] [age-in-days]
#
# Examples:
#   ./cleanup-old-cache.sh /mnt/ipcam-cache 30
#   ./cleanup-old-cache.sh /var/cache/ipcam-browser 7
#
# Crontab example (run daily at 3 AM):
#   0 3 * * * /path/to/cleanup-old-cache.sh /mnt/ipcam-cache 30 >> /var/log/ipcam-cache-cleanup.log 2>&1

CACHE_ROOT="${1:-/mnt/ipcam-cache}"
AGE_DAYS="${2:-30}"

# Validate cache root exists
if [ ! -d "$CACHE_ROOT" ]; then
    echo "Error: Cache root directory does not exist: $CACHE_ROOT" >&2
    exit 1
fi

# Log start
echo "[$(date -Iseconds)] Starting cache cleanup for: $CACHE_ROOT"
echo "[$(date -Iseconds)] Removing files older than $AGE_DAYS days"

# Count files before cleanup
FILES_BEFORE=$(find "$CACHE_ROOT" -type f | wc -l)
echo "[$(date -Iseconds)] Files before cleanup: $FILES_BEFORE"

# Find and remove files older than specified days
# -type f: only files (not directories)
# -mtime +N: modified more than N days ago
# -delete: delete matching files
DELETED=$(find "$CACHE_ROOT" -type f -mtime "+$AGE_DAYS" -delete -print | wc -l)

# Count files after cleanup
FILES_AFTER=$(find "$CACHE_ROOT" -type f | wc -l)

# Report results
echo "[$(date -Iseconds)] Deleted $DELETED files"
echo "[$(date -Iseconds)] Files after cleanup: $FILES_AFTER"
echo "[$(date -Iseconds)] Cache cleanup completed successfully"

# Exit with success
exit 0
