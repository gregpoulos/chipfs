#!/usr/bin/env bash
# ChipFS smoke test.
#
# Runs inside the chipfs-smoke Docker image (built with --target smoke-test).
# Requires the container to have been started with:
#   --cap-add SYS_ADMIN --device /dev/fuse
#
# Exits 0 if all checks pass, 1 otherwise.
set -euo pipefail

SOURCE=/testdata/fixtures
MOUNT=/mnt/chipfs
PASS=0
FAIL=0

# ── helpers ───────────────────────────────────────────────────────────────────

pass() { echo "  PASS  $*"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL  $*"; FAIL=$((FAIL + 1)); }

check_eq() {
    local label="$1" got="$2" want="$3"
    if [[ "$got" == "$want" ]]; then
        pass "$label"
    else
        fail "$label — got: '$got', want: '$want'"
    fi
}

# ── mount ─────────────────────────────────────────────────────────────────────

mkdir -p "$MOUNT"
chipfs -source "$SOURCE" -mountpoint "$MOUNT" &
CHIPFS_PID=$!

# Wait up to 5 seconds for the mount to appear.
for i in $(seq 1 25); do
    mountpoint -q "$MOUNT" 2>/dev/null && break
    sleep 0.2
done

if ! mountpoint -q "$MOUNT" 2>/dev/null; then
    echo "FATAL: chipfs did not mount within 5 seconds"
    kill "$CHIPFS_PID" 2>/dev/null || true
    exit 1
fi
echo "chipfs mounted at $MOUNT"
echo ""

# ── teardown ─────────────────────────────────────────────────────────────────

cleanup() {
    fusermount3 -u "$MOUNT" 2>/dev/null \
        || umount "$MOUNT" 2>/dev/null \
        || true
    wait "$CHIPFS_PID" 2>/dev/null || true
}
trap cleanup EXIT

# ── 1. root directory ─────────────────────────────────────────────────────────

echo "── 1. Root directory ────────────────────────────────────────────────────────"

for f in pently.nsf pently-demo.nsfe seaside-village.gbs ode-to-joy.spc; do
    [[ -f "$MOUNT/$f" ]] \
        && pass "real file '$f' present" \
        || fail "real file '$f' missing"
done

for d in pently pently-demo seaside-village ode-to-joy; do
    [[ -d "$MOUNT/$d" ]] \
        && pass "virtual dir '$d/' present" \
        || fail "virtual dir '$d/' missing"
done

# ── 2. track counts ───────────────────────────────────────────────────────────

echo ""
echo "── 2. Track counts ──────────────────────────────────────────────────────────"

check_eq "pently track count"                 "$(ls "$MOUNT/pently"       | wc -l | tr -d ' ')" "24"
check_eq "pently-demo track count (plst)"    "$(ls "$MOUNT/pently-demo"  | wc -l | tr -d ' ')" "10"
check_eq "ode-to-joy track count (SPC)"  "$(ls "$MOUNT/ode-to-joy" | wc -l | tr -d ' ')" "1"
check_eq "seaside-village track count (GBS)" "$(ls "$MOUNT/seaside-village" | wc -l | tr -d ' ')" "1"

# ── 3. WAV metadata via ffprobe (lazy emulation — no render triggered) ─────────

echo ""
echo "── 3. WAV metadata (header bytes + ffprobe) ─────────────────────────────────"

# check_wav_bytes reads the first 2 KB of a WAV file and greps for a string.
# Only reads the header region — never triggers emulation.
check_wav_bytes() {
    local label="$1" file="$2" pattern="$3"
    local found=0
    (set +o pipefail; head -c 2048 "$file" 2>/dev/null | grep -qa "$pattern") \
        && found=1 || true
    if [[ "$found" -eq 1 ]]; then
        pass "$label"
    else
        fail "$label (pattern '$pattern' not found in first 2048 bytes of $file)"
    fi
}

# probe runs ffprobe for format tags; -v error surfaces errors; || true
# prevents set -e from killing the script if ffprobe exits non-zero.
probe() {
    ffprobe -v error \
        -show_entries format_tags=title,artist,album \
        -of default=noprint_wrappers=1 \
        "$1" 2>&1 || true
}

# Primary: check raw header bytes (reliable, no render triggered).
check_wav_bytes "pently album tag (bytes)"  "$MOUNT/pently/Track_01.wav"     "Pently demo"
check_wav_bytes "pently artist tag (bytes)" "$MOUNT/pently/Track_01.wav"    "DJ Tepples"
pently_demo_first=$(ls "$MOUNT/pently-demo" | head -1)
check_wav_bytes "pently-demo album tag (bytes)" "$MOUNT/pently-demo/$pently_demo_first" "Pently demo"
ode_first=$(ls "$MOUNT/ode-to-joy" | head -1)
check_wav_bytes "ode-to-joy title (bytes)"    "$MOUNT/ode-to-joy/$ode_first"   "Ode"

# Advisory: also validate via ffprobe so we know a real media tool can read tags.
pently_probe=$(probe "$MOUNT/pently/Track_01.wav")
[[ "$pently_probe" == *"album=Pently demo"* ]] \
    && pass "pently album tag (ffprobe)" \
    || fail "pently album tag (ffprobe) — got: ${pently_probe:-<empty>}"
[[ "$pently_probe" == *"artist=DJ Tepples"* ]] \
    && pass "pently artist tag (ffprobe)" \
    || fail "pently artist tag (ffprobe) — got: ${pently_probe:-<empty>}"

pently_demo_probe=$(probe "$MOUNT/pently-demo/$pently_demo_first")
[[ "$pently_demo_probe" == *"album=Pently demo"* ]] \
    && pass "pently-demo album tag (ffprobe)" \
    || fail "pently-demo album tag (ffprobe) — got: ${pently_demo_probe:-<empty>}"

ode_probe=$(probe "$MOUNT/ode-to-joy/$ode_first")
[[ "$ode_probe" == *"title=Ode To Joy"* ]] \
    && pass "ode-to-joy title tag (ffprobe)" \
    || fail "ode-to-joy title tag (ffprobe) — got: ${ode_probe:-<empty>}"

# ── 4. file size invariant (stat size == rendered byte count) ─────────────────
#
# Reads the complete WAV file, triggering full emulation and caching.
# Chiptune emulation runs much faster than real-time so this is quick.
# Also verifies that backward seeks (implicit in wc -c) work from cache.

echo ""
echo "── 4. File size invariant (full render + cache) ─────────────────────────────"

size_check() {
    local wav="$1" label="$2"
    local stat_size read_size
    stat_size=$(stat -c %s "$wav")
    read_size=$(wc -c < "$wav")
    check_eq "$label: stat size == read size ($stat_size bytes)" \
        "$read_size" "$stat_size"
}

# Use ode-to-joy (SPC) and pently track 1 to exercise both formats.
size_check "$MOUNT/ode-to-joy/$ode_first" "ode-to-joy (SPC)"
size_check "$MOUNT/pently/Track_01.wav" "pently track 01 (NSF)"

# ── 5. -allow_other flag ──────────────────────────────────────────────────────
#
# Unmount the current instance and remount with -allow_other to verify the
# flag is accepted and the mount is functional. The runtime image already has
# user_allow_other enabled in /etc/fuse.conf (set during Docker build).

echo ""
echo "── 5. -allow_other flag ─────────────────────────────────────────────────────"

fusermount3 -u "$MOUNT" 2>/dev/null || umount "$MOUNT" 2>/dev/null || true
wait "$CHIPFS_PID" 2>/dev/null || true

chipfs -source "$SOURCE" -mountpoint "$MOUNT" -allow_other &
CHIPFS_PID=$!

for i in $(seq 1 25); do
    mountpoint -q "$MOUNT" 2>/dev/null && break
    sleep 0.2
done

if ! mountpoint -q "$MOUNT" 2>/dev/null; then
    fail "-allow_other: chipfs did not mount within 5 seconds"
else
    pass "-allow_other: mount succeeded"
    [[ -d "$MOUNT/pently" ]] \
        && pass "-allow_other: virtual directory accessible" \
        || fail "-allow_other: virtual directory not accessible"
fi

# ── summary ───────────────────────────────────────────────────────────────────

echo ""
echo "────────────────────────────────────────────────────────────────────────────"
TOTAL=$((PASS + FAIL))
echo "Results: $PASS/$TOTAL passed"
if [[ "$FAIL" -eq 0 ]]; then
    echo "All tests passed."
    exit 0
else
    echo "$FAIL test(s) failed."
    exit 1
fi
