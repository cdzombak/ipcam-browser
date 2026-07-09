#!/usr/bin/env bash
set -euo pipefail

# cleanup-old-cache.sh
# Removes cached media files older than 30 days from ipcam-browser cache directories
#
# The cache holds converted videos (*.mp4), generated video thumbnails (*.thumb.jpg),
# and camera images (*.jpg). All share the same retention age.
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

# Age, in days, after which an orphaned temp file is considered abandoned.
# ipcam-browser writes conversions to temp-* files before renaming them into place,
# so a crash mid-conversion strands one. Keep this well clear of any in-flight work.
TEMP_AGE_DAYS=1

# Validate cache root exists
if [ ! -d "$CACHE_ROOT" ]; then
    echo "Error: Cache root directory does not exist: $CACHE_ROOT" >&2
    exit 1
fi

# count_matching PATTERN <<< LIST
# Counts lines matching an extended regex. grep exits 1 when nothing matches,
# which would abort the script under `set -e`, so swallow that.
count_matching() {
    grep -cE "$1" || true
}

# Log start
echo "[$(date -Iseconds)] Starting cache cleanup for: $CACHE_ROOT"
echo "[$(date -Iseconds)] Removing files older than $AGE_DAYS days"

# Count files before cleanup
FILES_BEFORE=$(find "$CACHE_ROOT" -type f | wc -l)
echo "[$(date -Iseconds)] Files before cleanup: $FILES_BEFORE"

# Reap abandoned temp files first, so they aren't counted in the breakdown below
TEMP_DELETED=$(find "$CACHE_ROOT" -type f -name 'temp-*' -mtime "+$TEMP_AGE_DAYS" -print -delete | wc -l)
echo "[$(date -Iseconds)] Deleted $TEMP_DELETED abandoned temp files"

# Find and remove files older than specified days
# -type f: only files (not directories)
# -mtime +N: modified more than N days ago
# -print -delete: print the path, then delete it
DELETED_LIST=$(find "$CACHE_ROOT" -type f -mtime "+$AGE_DAYS" -print -delete)

# Break the deleted files down by type. Thumbnails end in .thumb.jpg, so they must be
# matched (and excluded from the image count) before the generic .jpg pattern.
DELETED=$(count_matching '.' <<<"$DELETED_LIST")
DELETED_VIDEOS=$(count_matching '\.mp4$' <<<"$DELETED_LIST")
DELETED_THUMBS=$(count_matching '\.thumb\.jpg$' <<<"$DELETED_LIST")
DELETED_IMAGES=$({ grep -vE '\.thumb\.jpg$' <<<"$DELETED_LIST" || true; } | count_matching '\.(jpg|jpeg|png)$')

# Count files after cleanup
FILES_AFTER=$(find "$CACHE_ROOT" -type f | wc -l)

# Report results
echo "[$(date -Iseconds)] Deleted $DELETED files ($DELETED_VIDEOS videos, $DELETED_THUMBS thumbnails, $DELETED_IMAGES images)"
echo "[$(date -Iseconds)] Files after cleanup: $FILES_AFTER"
echo "[$(date -Iseconds)] Cache cleanup completed successfully"

# Exit with success
exit 0
