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

for f in smb.nsf ducktales.nsfe kirby.gbs frogs-theme.spc; do
    [[ -f "$MOUNT/$f" ]] \
        && pass "real file '$f' present" \
        || fail "real file '$f' missing"
done

for d in smb ducktales kirby frogs-theme; do
    [[ -d "$MOUNT/$d" ]] \
        && pass "virtual dir '$d/' present" \
        || fail "virtual dir '$d/' missing"
done

# ── 2. track counts ───────────────────────────────────────────────────────────

echo ""
echo "── 2. Track counts ──────────────────────────────────────────────────────────"

check_eq "smb track count"               "$(ls "$MOUNT/smb"       | wc -l | tr -d ' ')" "18"
check_eq "ducktales track count (plst)"  "$(ls "$MOUNT/ducktales" | wc -l | tr -d ' ')" "16"
check_eq "frogs-theme track count (SPC)" "$(ls "$MOUNT/frogs-theme" | wc -l | tr -d ' ')" "1"

kirby_count=$(ls "$MOUNT/kirby" | wc -l | tr -d ' ')
[[ "$kirby_count" -gt 0 ]] \
    && pass "kirby has $kirby_count tracks" \
    || fail "kirby has no tracks"

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
check_wav_bytes "smb album tag (bytes)"    "$MOUNT/smb/Track_01.wav"        "Super Mario Bros"
check_wav_bytes "smb artist tag (bytes)"   "$MOUNT/smb/Track_01.wav"        "Koji Kondo"
duck_first=$(ls "$MOUNT/ducktales" | head -1)
check_wav_bytes "ducktales album tag (bytes)" "$MOUNT/ducktales/$duck_first" "DuckTales"
frog_first=$(ls "$MOUNT/frogs-theme" | head -1)
check_wav_bytes "frogs-theme title (bytes)"   "$MOUNT/frogs-theme/$frog_first" "Frog"

# Advisory: also validate via ffprobe so we know a real media tool can read tags.
smb_probe=$(probe "$MOUNT/smb/Track_01.wav")
[[ "$smb_probe" == *"album=Super Mario Bros."* ]] \
    && pass "smb album tag (ffprobe)" \
    || fail "smb album tag (ffprobe) — got: ${smb_probe:-<empty>}"
[[ "$smb_probe" == *"artist=Koji Kondo"* ]] \
    && pass "smb artist tag (ffprobe)" \
    || fail "smb artist tag (ffprobe) — got: ${smb_probe:-<empty>}"

duck_probe=$(probe "$MOUNT/ducktales/$duck_first")
[[ "$duck_probe" == *"album=DuckTales"* ]] \
    && pass "ducktales album tag (ffprobe)" \
    || fail "ducktales album tag (ffprobe) — got: ${duck_probe:-<empty>}"

frog_probe=$(probe "$MOUNT/frogs-theme/$frog_first")
[[ "$frog_probe" == *"title="* ]] \
    && pass "frogs-theme title tag (ffprobe)" \
    || fail "frogs-theme title tag (ffprobe) — got: ${frog_probe:-<empty>}"

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

# Use frogs-theme (SPC) and smb track 1 to exercise both formats.
size_check "$MOUNT/frogs-theme/$frog_first" "frogs-theme (SPC)"
size_check "$MOUNT/smb/Track_01.wav" "smb track 01 (NSF)"

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
    [[ -d "$MOUNT/smb" ]] \
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
