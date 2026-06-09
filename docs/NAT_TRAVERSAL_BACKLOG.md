# LwyLink NAT Traversal Backlog

## 2026-06-09 Updated Priority

Research from Tailscale, NetBird, ZeroTier, Nebula, libp2p, WebRTC, and TURN points to the same product lesson: NAT traversal should be treated as a candidate system with reliable fallback, not as a one-shot punch success/failure flag.

For LwyLink, the next best work is:

1. Keep the current overlay relay-first behavior stable.
2. Add path diagnostics before adding more aggressive punching.
3. Continue improving STUN server-reflexive candidates in the native UDP overlay.
4. Add TURN for WebRTC voice, because WebRTC without TURN is not a complete voice product.
5. Only then consider UPnP/NAT-PMP/PCP, multi-relay selection, or QUIC/UDP relay.

Do not optimize for a higher direct percentage if the result causes path flapping. The user-facing win is “stable and explainable,” then “direct when possible.”

## TURN Deployment Note

For WebRTC voice, use coturn REST credentials instead of static client passwords:

- coturn enables `use-auth-secret`.
- coturn and LwyLink share the same `static-auth-secret`.
- LwyLink exposes only short-lived `username/credential` pairs through `/api/rooms/voice/webrtc/ice`.
- `voiceTURNCredential.urls` should include the public `turn:` and optionally `turns:` addresses.
- The static auth secret must stay in ignored production config or environment variables.

This keeps the long-lived TURN secret out of the Windows client package and reduces abuse risk if users inspect app traffic.

This backlog turns the research notes into implementation candidates. It focuses on improving the existing LwyLink overlay rather than replacing it.

## Current Model

LwyLink already has a useful shape:

- Control service authenticates users and rooms.
- Node service provides overlay TCP relay and UDP rendezvous.
- Clients connect through TCP relay first.
- UDP direct is attempted as an optimization.
- UDP failure falls back to TCP relay.
Voice uses WebRTC media; the overlay now focuses on virtual LAN IP traffic.

This should remain the core model. STUN server-reflexive candidate discovery is now implemented; the main missing parts are richer candidate-pair state and better diagnostics.

## Mature Project Patterns To Borrow

### From Tailscale

- Relay is always available and boring.
- Direct UDP is an upgrade, not the baseline assumption.
- Connection type should be observable.
- Path changes should be stable, not twitchy.

### From NetBird / ICE

- Try multiple candidate types.
- Keep candidate-pair state and selected pair state.
- Include relay as a candidate, not just a fallback branch.
- Measure RTT and failure reasons.

### From ZeroTier / Nebula

- Have a clear discovery authority.
- Let public nodes help peers find each other.
- Use LAN discovery and NAT keepalive carefully.
- Support explicit relays for difficult networks.

## Candidate Types

LwyLink should gradually model these candidate types:

| Type | Meaning | Priority |
| --- | --- | ---: |
| `host-lan` | local LAN endpoint | 100 |
| `host-vpn` | local virtual/other interface endpoint | 70 |
| `srflx-stun` | STUN server-reflexive endpoint | 90 |
| `node-observed` | endpoint observed by LwyLink node | 80 |
| `relay-tcp` | current TCP overlay relay | 10 |
| `relay-udp` | future UDP/QUIC relay | 20 |

The priority table is not final. It exists so tests and UI can discuss candidates consistently.

## P0: Diagnostics Before More Punching

Add path diagnostics first. Without this, new punching features are hard to evaluate.

For every peer pair, record:

- peer user ID.
- peer virtual IP.
- candidate pair type.
- selected path.
- last probe timestamp.
- last success timestamp.
- last failure timestamp.
- last RTT.
- consecutive failures.
- disabled-this-run reason.

Expose diagnostics in:

- smoke JSON artifacts.
- hidden client diagnostics panel.
- admin page later.

## P1: STUN Server-Reflexive Candidate

Current status: implemented as an automatic UDP overlay candidate source. The client sends a short-timeout STUN Binding request from the same UDP socket used for rendezvous, reports the discovered `srflx-stun` endpoint to the node, and peers probe it alongside LAN and node-observed endpoints. Overlay UDP nodes also answer minimal STUN Binding requests, so the default STUN target is the current room node UDP address rather than a public third-party STUN server.

Keep improving STUN discovery:

- Client sends binding request to configured STUN server.
- Client reports srflx endpoint to node/control path.
- Peers probe srflx endpoint alongside node-observed endpoint.
- Production deployments should prefer controlled LwyLink STUN/TURN hosts; public STUN is only a development fallback.

Benefits:

- Better visibility into NAT mapping.
- Often improves home NAT direct success.
- Helps distinguish UDP blocked vs mapping mismatch.

Risks:

- Public STUN dependency.
- Some symmetric NAT cases still fail.
- Need rate limits and timeouts.

## P2: Candidate Pair State Machine

Current direct path logic should evolve toward candidate-pair state:

States:

- `new`
- `probing`
- `succeeded`
- `selected`
- `failed`
- `cooldown`
- `disabled-this-run`

Important behavior:

- Do not switch selected path on one success if relay is currently stable.
- Do not flap between relay and direct.
- Re-probe when a new peer joins or local network changes.
- Stop aggressive probing after repeated failure.

## P3: Optional Local Port Mapping

Try UPnP/NAT-PMP/PCP later:

- Only after clear product review, because UDP P2P probing is automatic.
- Only in private LAN contexts.
- Never block room join on it.
- Treat success as one more candidate.

Benefits:

- Can improve direct success for some routers.

Risks:

- Router compatibility varies.
- Security-sensitive; must be transparent.
- Not suitable for all users.

## P4: UDP or QUIC Relay

Current TCP relay is stable, but high-throughput or latency-sensitive virtual LAN traffic can still suffer head-of-line blocking.

Investigate:

- UDP relay for selected overlay payloads.
- QUIC datagrams if available.
- Keep TCP relay for IP/control fallback.

Benefits:

- Better latency behavior under loss/jitter.
- Avoids one large IP frame delaying later latency-sensitive packets.

Risks:

- More server complexity.
- More firewall issues.
- Traffic accounting needs care.

## P5: Multi-Node Relay Selection

When there are multiple nodes:

- Select low-latency node per room or peer pair.
- Keep room owner-selected node as default.
- Expose relay RTT and load.

This is later work, but the diagnostics model should not assume only one node forever.

## Immediate Experiments

1. Add smoke output for current UDP candidate attempts and selected path.
2. Compare direct success rate before/after STUN.
3. Add relay latency/jitter measurement to smoke.
4. Decide whether UDP/QUIC relay is needed for virtual LAN traffic after diagnostics show TCP relay head-of-line blocking in real use.

## Non-Goals For Now

- Full ICE implementation.
- TURN replacement inside LwyLink immediately.
- Multi-region relay optimization.
- NAT type detection as a user-facing promise.

## Decision Rule

Improve the LwyLink overlay for virtual LAN traffic. Voice now uses WebRTC, but better NAT traversal still benefits games and applications using the virtual network.
