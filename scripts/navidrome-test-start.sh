#!/usr/bin/env bash
# Navidrome integration test entrypoint.
#
# Starts chipfs (FUSE) then hands off to Navidrome in the same container,
# so Navidrome can read the virtual WAV files without any cross-container
# mount propagation needed.
set -euo pipefail

mkdir -p /mnt/chipfs
chipfs -source /chips -mountpoint /mnt/chipfs -allow_other &
CHIPFS_PID=$!

# Wait up to 5 seconds for the mount to appear.
for i in $(seq 1 25); do
    mountpoint -q /mnt/chipfs 2>/dev/null && break
    sleep 0.2
done

if ! mountpoint -q /mnt/chipfs 2>/dev/null; then
    echo "ERROR: chipfs did not mount within 5 seconds"
    kill "$CHIPFS_PID" 2>/dev/null || true
    exit 1
fi
echo "chipfs mounted at /mnt/chipfs"

cleanup() {
    fusermount3 -u /mnt/chipfs 2>/dev/null || umount /mnt/chipfs 2>/dev/null || true
    wait "$CHIPFS_PID" 2>/dev/null || true
}
trap cleanup EXIT

exec navidrome
