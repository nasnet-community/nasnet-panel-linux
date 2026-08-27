# Router mode bug fixes

Three branches were reviewed before they merged into `main`: router mode
(#55), the WireGuard VPN (#59) and the traffic flow page (#58). Every finding
below was verified against the code before it was fixed, and each one has a
regression test.

One more bug turned up afterwards on the test VM and is listed at the end.

---

## Router mode (#55)

**A port with no permanent MAC took over every interface.** `[Match]` files
were written with `PermanentMACAddress=` and nothing after it. systemd reads an
empty value as "clear the list", so the file matched *every* link on the box —
the LAN bridge and the management port included, each getting the WAN's DHCP
settings. Now such a port is matched by name instead. `8ec4c4e`

**A role that went away left its config file behind.** Files were written but
never removed, so turning the LAN off left the bridge unit in place. networkd
kept building the bridge at every boot, with the firewall rules that used to
protect it already gone. `c0e180a`

**Forwarding was switched on but never off.** Enabling the LAN set
`ip_forward=1`; disabling it left the value alone while removing the forward
filter and the NAT rules. The result was a box that still forwarded traffic
between uplinks with nothing filtering it. `b416100`

**A second apply disarmed the first one's safety net.** Every network change
arms a dead-man timer that reverts it unless confirmed. Starting a second
change overwrote that marker — so the first change, which might already have
cut the operator's connection, would never revert. Applying now refuses while
a change is still armed. `98de5d0`

**One overlapping address range could take the whole firewall down.** The
address sets are loaded in a single atomic transaction. Without `auto-merge`,
two overlapping prefixes in the upstream list make the kernel reject the entire
load. `417bcd0`

**Three of the six role choices could never succeed.** The panel sent only the
interface and the role, never the id of the interface being replaced, the
bridge a member joins, or the typed CONFIRM. So "LAN", "LAN member" and
"Management" always came back rejected, and the confirmation box did nothing.
`195c033`

**Cellular modems were invisible.** A QMI/MBIM modem reports `DEVTYPE=wwan`,
which the classifier read as "virtual device" — the same bucket as veth pairs —
so modems were hidden from the interface list and could not be given a role.
`b193001`

**Tethering phones were never recognised.** The detection matched the words
`mtp` and `adb`, but udev supplies those as hex codes (`ffff00`, `ff4201`).
The Android tethering branch was unreachable in production; only the test,
which built the data by hand, ever exercised it. `b4dc37f`

### CI

**Tests needed a database that is not in the repo.** The geoip database is too
large to commit, so CI substitutes empty files — and ten tests failed there
while passing locally. They now skip when the database is absent, and a real
parse failure still fails. `a2b422d`

**The test suite was driving the CI runner's own network.** `systemctl`,
`networkctl` and `netplan` do not exist on a developer's Mac, so those calls
quietly did nothing. On a Linux runner they are real, and the tests restarted
the runner's networking. The suite now runs with an empty `PATH`. `a39aaa9`

---

## VPN (#59)

**A rollback destroyed the VPN profile it was restoring.** The profile's
config is deliberately hidden from API responses, and that also kept it out of
the snapshot file. A rollback therefore restored a profile with an empty config
and wrote that over the real row — losing the private key, the peer and the
endpoint permanently. The tunnel could never come back. The config now travels
in its own snapshot field, and the database refuses to store a profile without
one. `3937030` `188fe32`

**A snapshot could silently skip the tunnel.** If reading the active profile
failed, the error was discarded and the snapshot looked complete. A later
rollback would then restore everything *except* the tunnel, leaving the
database and the kernel describing different states. `3937030`

**Foreign name lookups fell back to the system resolver.** With no tunnel up,
those lookups are supposed to go nowhere. dnsmasq was never told to stop
reading `/etc/resolv.conf`, so it quietly forwarded them to the domestic
resolver instead. `de3a4d3`

**One damaged profile hid all the others.** A single row whose config would not
decode failed the whole list request, so the VPN tab showed "no VPN yet" — and
the bad row could not even be deleted. Such a row is now listed and marked
unreadable. `7540fad`

**Deleting a profile that was already gone returned a server error.** Common
with two browser tabs open. It answers 404 now. `188fe32`

**A shared WireGuard link was often rejected.** Roughly half of all keys
contain a `/`, and unencoded that ends the address portion of the URL — so the
panel reported "no private key in the URI" for a link that plainly had one.
`2c88268`

**A split tunnel could silently become a full tunnel.** If every allowed range
was IPv6, the list fell back to "everything" without saying so, sending all
traffic through a tunnel the operator had scoped narrowly. `f2bed95`

**A failed read looked like a successful teardown.** Turning the VPN off
reported success even when the routing table could not be read, leaving the
tunnel's route in place. `0567623`

**The bootstrap resolver leaked a socket a minute.** It built a new HTTP client
per request and never closed the idle connections, while retrying every 60
seconds for as long as an endpoint stayed unreachable. `ed50ab8`

**xray-core was installed without checking what was downloaded.** The bundled
copy is checksum-verified; the download path unpacked a zip from the internet
and installed it as root with no verification at all. It now verifies against
the published digest, or a pinned one, and refuses if neither is available.
`eaba059`

**Warnings that arrive with a success were thrown away.** "This VPN only
carries 10.0.0.0/8", "no secondary uplink assigned" — the backend produced
them, the request succeeded, and the UI dropped them on the floor. `06c3470`

**The edit dialog always opened blank.** It read the profile once when it was
first created, before any profile was selected, so editing meant retyping
everything. `00342ca`

**The tunnel MTU had two defaults.** One was applied to the interface, another
reported by the status API, and changing one left the other lying. `41fae2e`

---

## Traffic flow page (#58)

This page exists to tell the operator the truth about where traffic goes, so
every bug here made it lie confidently.

**Anything not in the tunnel was reported as leaving via the domestic uplink.**
Even when the route plainly named the other interface. `d21cedc`

**The most dangerous disagreement went unreported.** When the page's own walk
found no route but the kernel had one, it said nothing — and then declared the
traffic dropped. `d21cedc`

**A failed read was reported as a configuration error.** If a routing table
could not be read, the page said the table had no default route and blamed the
setup for something it simply could not see. `a672236`

**A missing address set made every domestic address look foreign.** The kernel
returns the same "no" for "not in this set" and "that set does not exist".
`3df61bc`

**The "byte counters are off" warning was hidden exactly when it applied.** If
the setting could not be read, the page assumed counting was on — so the
operator saw a table of zeroes with no explanation. `0e88955`

**A missing rule blamed the wrong uplink.** The red marker landed on the
secondary uplink for a rule belonging to the domestic one. `19652b7`

**Other software's routing rules were flagged as problems.** Anything in a very
wide range was called unexpected, so a Tailscale or libvirt rule produced a
permanent warning that no amount of fixing could clear — training the operator
to ignore the warnings that matter. `a05b8a0`

**A firewall state with only counters counted as empty.** A rollback would
therefore tear the table down instead of restoring it. `2f5d4b2`

**A missing counter was never reported.** The check existed but nothing called
it, so the graph drew blank throughput while insisting everything matched.
`0484dc7`

**Export produced no file in Firefox**, on the feature whose whole purpose is
attaching state to a bug report. `3057058`

**Every error read as "router mode is not enabled".** Including a genuine
backend failure — on the page you open to diagnose failures. `df251ae`

**The two most expensive probes ran twice per request**, on a page that polls
every three seconds. `718bf92`

**Throughput was attached to graph edges by position**, so inserting an edge
would silently move the numbers onto the wrong arrow. `9f92083`

**The event ring could crash the process** if it was ever created with a size
of zero or less. `45405ae`

---

## Found afterwards, on the test VM

**A rollback that could not succeed retried forever.** The VM had an
unconfirmed change whose snapshot could not be restored. Because the failure
path never disarmed the marker, the timer replayed the same doomed restore
every ten seconds — each attempt writing a stale rule set back over whatever
the panel had just reconciled. The traffic flow page found it: it reported one
rule missing and four unexpected, which was the panel and the timer fighting
over the routing rules.

The restore now gets three attempts, then disarms and marks the change failed.
`14e7403`

---

## Looked at and deliberately left alone

**The input firewall and DHCP.** Reported as dropping the box's own DHCP
renewals. It does not: the DHCP client uses a raw socket that the firewall
never sees, and lease renewals are ordinary replies to traffic the box itself
started.

**Bandwidth shaping clearing the connection marks.** Real, but shaping is out
of scope for this work.

**Dumping the whole connection table every three seconds.** The expensive part
is reading the table, not sorting it, so a bounded selection would add
complexity without buying anything measurable.
