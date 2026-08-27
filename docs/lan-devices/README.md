# LAN connected devices — design Q&A

The design interview that produced the feature, in the order it was asked. Every
question lists the options that were on the table and the answer that was given,
so a later reader can tell a *decision* from an *accident*.

Seven rounds. A round only asked what the previous answers had unblocked.

Related: the implementation plan and its decision table live in
`docs/superpowers/plans/2026-08-11-router-mode-lan-devices.md`.

---

## The two questions that started it

**How many devices can connect through a single LAN port?**

One cable is one device — but the port is a bridge member, so a switch behind it
puts N devices on `lan0`. Measured limits on the target:

| Bound | Value | Note |
| --- | --- | --- |
| **DHCP pool (as shipped)** | **101** | `10.77.0.100`–`.200`. The real ceiling. |
| `/24` total usable | 253 | statics outside the pool still work |
| ARP `gc_thresh3` | 1024 | not close |
| Bridge FDB `hash_max` | 4096 | not close |

A **router** behind the port (rather than a switch) shows as **one MAC** and
hides everything behind its NAT. No workaround exists.

**What are the caveats?** Thirteen, listed in the plan. The three that shaped the
design:

1. Leases are wrong in both directions — a 12 h ghost after unplugging, and a
   static-address device never appears at all.
2. **Neighbour state cannot express liveness here.** `base_reachable_time_ms` is
   30000 randomized 0.5–1.5×, so REACHABLE decays to STALE in 15–45 s of idle;
   and 101 devices sits under `gc_thresh1` (128), so neighbour GC never runs.
   STALE→offline lies about idle devices; STALE→online lies forever about
   departed ones.
3. MAC randomization makes identity unstable the moment Wi-Fi lands.

---

## Round 1 — the root

**Q1. What is this for?**
Read-only observability, or the data foundation for per-device policy?
→ **Read-only observability.** Per-device policy needs source-based marking;
`LANClassify` marks by destination. Different feature.

**Q2. What counts as "connected"?**
(a) leases only · (b) neighbour + FDB only · (c) union
→ **(c) union.** (a) is wrong in both directions; (b) gives a table of bare MACs
with no names.

**Q3. Wired-only, or wireless-ready identity?**
→ **Wired-only, MAC as the key**, with randomized MACs flagged. Wi-Fi arrives in
stage 3; the plan was updated to record what that will break.

---

## Round 2

**Q4. Does "read-only" mean stateless?**
(a) pure stateless · (b) stateless + an inert annotation table · (c) a stored
device table
→ **(b).** Compared in full below.

**Q5. Liveness state model.**
(a) binary online/offline · (b) three states (Online / Recently seen / Known) ·
(c) raw kernel state
→ **(a) binary.** Chosen against the recommendation of (b); the consequence is
handled by Q8.

**Q6. Pin `dhcp-leasefile=` in the render?**
→ **Yes**, to the existing default `/var/lib/misc/dnsmasq.leases`. We were
reading a path we never set.

**Q7. How much of the stage-3 Wi-Fi plan to edit?**
1 bridge note · 2 randomized-MAC handling · 3 hostapd enrichment · 4 opaque ID
→ **1 and 2 now, 3 as a marked stretch, 4 deferred with the reason recorded.**

### Q4 in full — the comparison that decided it

Scale ceiling is 101 devices, so every performance argument for caching is dead.
The schema is **additive-only** (`internal/network/domain/interface.go:5`), so a
table added here can never be cleanly reshaped or dropped.

| | A. Pure stateless | B. Stateless + annotation table | C. Stored device table |
| --- | --- | --- | --- |
| MAC, IP, hostname, liveness | ✅ | ✅ | ✅ |
| Nicknames | ❌ | ✅ | ✅ |
| First seen / history | ❌ | ❌ | ✅ |
| Ghost accumulation | impossible | only rows a user typed | **needs a retention rule** |
| Snapshot/rollback | nothing to decide | notes are user data → exclude | **must decide** |
| A source goes missing | degrades per field | same | **stale rows look live** |
| Reversible | ✅ | ✅ (empty table is inert) | ❌ permanent |

Where each breaks: **A** — "which one is the NAS?" has no answer but a MAC, and
that complaint arrives on day one. **B** — no "when did this first appear?".
**C** — when dnsmasq is down the table keeps serving rows that look
authoritative; A and B cannot lie that way, because no source means no row.

---

## Round 3

**Q8. The online predicate, given a binary model.**
(a) neighbour state · (b) FDB age · (c) FDB age OR neighbour reachable
→ **(b): online ⟺ the MAC is in the `lan0` FDB with `updated` < the bridge's own
`ageing_time`, read from sysfs rather than hardcoded.**

This is the pivotal decision. Measurement showed a binary built on neighbour
state is broken *both* ways — STALE→offline marks an idle laptop offline after
~30 s; STALE→online never marks anything offline, because we sit below
`gc_thresh1`. The FDB ages deterministically and `bridge -s -j fdb` reports the
age as a number.

Accepted consequence: **up to 5 minutes of lag before a device reads offline.**
That is the price of a binary model; there is no shorter honest threshold.

**Q9. Annotation table shape.**
→ **`lan_device_label`: `mac` (unique) + `label`, nothing else. Orphans kept
forever. Excluded from `Snapshot`.**

**Q10. Collection and refresh.**
per request, or a background poller with a cache?
→ **Per request. No cache, no poller.** This makes `dhcp-script=` moot.

**Q11. OUI vendor lookup in scope?**
(a) no · (b) embedded table · (c) fetched and cached
→ **(b) embedded, 24-bit prefix.** A randomized MAC must render as "randomized",
never as a wrong vendor.

---

## Round 4

**Q12. Degraded behaviour.**
(a) 404/error · (b) always 200 with an empty list · (c) always 200 + per-source
health flags
→ **(c)**, mirroring the existing `LANView.ResolverReady` pattern. An
unexplained empty list is indistinguishable from "nothing is connected", which
is the question the feature exists to answer.

**Q13. Hostname sanitization.**
(a) render raw · (b) RFC-1123 charset only, else empty · (c) strip to a safe
charset
→ **(b).** React escapes, so the risk is **spoofing**, not XSS: homographs, RTL
overrides, control characters. Stripping is worse than rejecting — a mangled
name still looks legitimate.

**Q14. Surface which member port a device is behind?**
→ **Yes.** Free in the same FDB output. Only discriminating when several ports
are bridge members: with a switch behind one port, everything reports that port.

---

## Round 5

**Q15. API shape.**
(a) `GET /network/lan/devices` + a label endpoint · (b) fold into `GET
/network/lan` · (c) top-level `/network/devices`
→ **(a).** (b) welds two very different refresh rates together.

Corrected during the round: the MAC goes in the **path**, not the body —
`PUT /network/lan/devices/:mac/label` — mirroring the existing
`PUT /interfaces/:key` → `SetLabel` (`handler.go:86`).

**Q16. UI placement and refresh.**
(a) a fourth tab · (b) a section in the Local network tab · (c) a card on the
index
→ **(b)**, in its own component file. Poll every 10 s, paused when the tab is
hidden. The asymmetry must be stated in the UI: arrivals are instant, departures
lag.

**Q17. Collector seam.**
(a) extend `Backend` · (b) a narrow `DeviceSource` · (c) exec inline with golden
fixtures
→ **(b) + (c), split by concern.** `Backend` is the apply/rollback seam;
read-only observation does not belong on it. Pure parsers hold all the real
risk.

---

## Round 6

**Q18. Which embed pattern for the OUI table?**
→ **The `geofiles` pattern: committed, unconditional `go:embed`, no build tag.**
A build tag would leave most builds silently without a vendor column. The raw
CSV is not committed; it compiles to a 377 KB table of 39,913 assignments.

**Q19. Label constraints.**
→ **Trim, reject control characters, cap at 63 runes, allow Unicode.**
Deliberately different from Q13: a hostname is attacker-supplied, a label is
operator-supplied and may legitimately be Persian.

**Q20. Does "orphans live forever" survive Wi-Fi?**
(a) keep forever · (b) cap and reap · (c) refuse to label a randomized MAC
→ **(c), keeping (a) for real MACs.** Kills the growth vector at the source, and
the refusal is informative — it teaches the operator that their phone
randomizes.

**Q21. What proves it works?**
→ **Parser golden tests from real captured output.**

---

## Round 7

**Q22. Verification scope — golden tests only, or golden tests plus a live check?**
→ **Both.** On this branch, every one of ~12 defects was found on real hardware
and was invisible to 1000+ passing tests.

**Q23. A plan document first, or straight to code?**
(a) a full stage plan · (b) a short plan — decisions plus tasks · (c) straight to
code
→ **(b).** The reasoning is the durable part; the prose is not.

---

## Two asymmetries worth keeping straight

- **Hostname is ASCII-locked; a label allows Unicode.** Attacker-supplied versus
  operator-supplied. Deliberate, not an inconsistency.
- **Arrivals are instant; departures lag up to `ageing_time`.** A device enters
  the FDB on its first frame; a departed one waits out ageing. Stated in the UI,
  or it reads as a bug.

## Deferred, and what would force each

| Deferred | Only forced by |
| --- | --- |
| Opaque device ID + MAC history | per-device policy, which Q1 excluded |
| First seen / history | the permanent device table Q4 rejected |
| Per-device routing policy | source-based marking; a new mark field and rule prefs |
| hostapd RSSI / band / per-station bytes | stage 3, marked as a stretch |
| Validation on the existing interface `SetLabel` | a real gap, out of scope here |
