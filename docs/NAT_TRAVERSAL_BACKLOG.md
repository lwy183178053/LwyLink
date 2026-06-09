# LwyLink NAT Traversal Backlog

This backlog turns the research notes into implementation candidates. It focuses on improving the existing LwyLink overlay rather than replacing it.

## Current Model

LwyLink already has a useful shape:

- Control service authenticates users and rooms.
- Node service provides overlay TCP relay and UDP rendezvous.
- Clients connect through TCP relay first.
- UDP direct is attempted as an optimization.
- UDP failure falls back to TCP relay.
- Voice and IP use distinct overlay frame types.

This should remain the core model. The main missing parts are richer candidate discovery and better diagnostics.

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

Add optional STUN discovery:

- Client sends binding request to configured STUN server.
- Client reports srflx endpoint to node/control path.
- Peers probe srflx endpoint alongside node-observed endpoint.

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

- Only if user enables P2P.
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

Current TCP relay is stable, but realtime voice over TCP can suffer head-of-line blocking.

Investigate:

- UDP relay for voice frames.
- QUIC datagrams if available.
- Keep TCP relay for IP/control fallback.

Benefits:

- Better voice under loss/jitter.
- Avoids one large IP frame delaying voice frames.

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
2. Add STUN srflx candidate behind config flag.
3. Compare direct success rate before/after STUN.
4. Add voice relay jitter measurement to smoke.
5. Decide whether UDP/QUIC relay is needed for voice if WebRTC is not adopted.

## Non-Goals For Now

- Full ICE implementation.
- TURN replacement inside LwyLink immediately.
- Multi-region relay optimization.
- NAT type detection as a user-facing promise.

## Decision Rule

Improve the LwyLink overlay for virtual LAN traffic regardless of the voice decision. Even if voice moves to WebRTC, better NAT traversal still benefits games and applications using the virtual network.
