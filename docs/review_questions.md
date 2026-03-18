# Questions for External Reviewer

These are open questions where outside expertise would improve confidence in the
current implementation. They are not known bugs — they are design and compatibility
decisions where a second opinion matters.

---

## 1. RIFF `id3 ` chunk: is this the right tag format for broad media server compatibility?

ChipFS embeds ID3v2 tags inside a RIFF `id3 ` chunk (per the RIFF extension
spec). Navidrome (taglib) and ffprobe read it correctly. But some players expect
ID3v2 at the *end* of the file, and some older or stricter parsers look only for
`LIST INFO` RIFF chunks.

**Question:** Is the `id3 ` RIFF chunk approach the right long-term choice, or
should we also (or instead) write a `LIST INFO` chunk for maximum compatibility?
Are there common players or media servers that would fail to read our tags?

---

## 2. libgme CGO version shim: has this actually been validated on Debian bookworm?

Homebrew ships libgme 0.6.4 (has `gme_set_fade_msecs` and `gme_info_t.fade_length`).
Debian bookworm ships 0.6.3 (neither). The CGO preamble uses a version-gated shim
to bridge the difference. The shim was written against the API documentation, and
the Docker build compiles on bookworm — but the fade behaviour under 0.6.3 has
not been audited against real files.

**Question:** Does the 0.6.3 fallback path produce correct fade behaviour, or
does the absence of `gme_set_fade_msecs` cause tracks to either not fade at all
or cut off abruptly? Is there a better version-detection approach than the current
preprocessor guard?

---

## 3. Cache sizing and eviction policy

The LRU cache defaults to 256 MB. At 44.1 kHz stereo 16-bit, a 2.5-minute track
is ~26 MB, so the cache holds roughly 10 tracks. For a game with 18 tracks (e.g.
SMB), a user seeking around the full album would thrash the cache continuously.

LRU is the natural default, but chiptune access patterns may be unusual: a user
is likely to listen to all tracks in a game sequentially, then switch games
entirely — which is closer to a scan pattern than a recency pattern.

**Question:** Is LRU the right eviction policy here, or would LFU or a per-source-
file grouping (evict whole games at once) fit the access pattern better? Is 256 MB
a reasonable default for a home server, or should it be lower?

---

## 4. Security posture of `-allow_other` + `sanitizeFilename`

With `-allow_other` set, any process sharing the host (or a Docker container with
the mount bound in) can read the filesystem. The virtual paths are constructed
from filenames found on disk, processed through `sanitizeFilename`, which only
strips `/`, `\x00`, and `:`.

**Question:** Are there filename characters or sequences that `sanitizeFilename`
should also strip or escape to prevent unexpected behaviour from FUSE clients?
Are there path traversal or symlink-following risks in the way `RealFile` and
`ChipDir` are rooted at the source directory? Should the source directory be
validated more strictly at mount time (e.g. reject if it contains symlinks that
escape the root)?
