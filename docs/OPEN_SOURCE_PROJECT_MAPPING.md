# LwyLink Open Source Project Mapping

This document maps mature products and open-source projects to concrete LwyLink modules. It is a guardrail against shallow copying: each project is useful for some parts and dangerous for others.

## Mapping Table

| Project | What It Is | Useful For LwyLink | Do Not Copy Blindly |
| --- | --- | --- | --- |
| Tailscale | WireGuard mesh with DERP relay fallback | Relay-first mindset, path observability, stable direct upgrades | Full tailnet identity model; LwyLink rooms are simpler and more temporary |
| NetBird | WireGuard management plane using ICE-like connectivity | Candidate gathering, STUN/relay thinking, management-plane diagnostics | Full enterprise device management surface |
| ZeroTier | Virtual Ethernet overlay with roots/moons | Root/node discovery model, long-lived virtual networks, relay fallback | Its full VL1 protocol and global identity model |
| Nebula | Certificate-based mesh VPN with lighthouse discovery | Lighthouse pattern, explicit relay, punchy keepalive | Static config-heavy UX |
| RustDesk | Self-hostable remote desktop with rendezvous/relay | Rendezvous + relay product shape, custom server support, real-world NAT fallback | Remote desktop media stack and AGPL code reuse |
| Parsec | Low-latency game/desktop streaming | Latency discipline, UPnP + STUN + P2P mindset, dynamic network adaptation | Closed custom streaming protocol and hardware video pipeline |
| Sunshine/Moonlight | Self-hosted low-latency game streaming | Local pairing, low-latency streaming product UX, device compatibility lessons | Video-streaming architecture; LwyLink is virtual LAN first |
| libp2p | Modular peer-to-peer networking stack | Relay-as-transport, DCUtR-style upgrade through relay, AutoNAT idea | DHT/gossip/content routing complexity |
| WebRTC | Browser/native real-time media stack | Voice baseline: ICE/STUN/TURN, Opus, NetEQ, AEC, stats | Letting WebRTC replace the whole virtual LAN overlay |
| Mumble | Low-latency voice chat | Voice-room product details, Opus voice expectations, channel/mic semantics | Centralized voice-server architecture if P2P voice remains desired |

## Project Notes

### RustDesk

Why it matters:

- It is close to LwyLink in product shape: client app, self-hostable infrastructure, rendezvous/relay, and encrypted peer sessions.
- Its README explicitly exposes rendezvous/relay server concepts and points to `rendezvous_mediator` as peer connection coordination code.

What LwyLink should learn:

- Make custom control/node infrastructure easy to configure.
- Expose whether direct or relay is being used.
- Treat relay as normal user experience, not as "bad mode".
- Keep NAT troubleshooting logs available.

What not to copy:

- Remote desktop media stack. LwyLink is not a remote desktop product.
- AGPL code directly.

### Parsec

Why it matters:

- It is a commercial example of prioritizing low-latency user experience.
- Public docs emphasize native low-level control, reliable UDP-style streaming, dynamic bitrate, P2P NAT traversal, UPnP and STUN.

What LwyLink should learn:

- Latency is a product metric, not an implementation detail.
- UPnP/STUN can be part of practical P2P success, not just theory.
- Network adaptation should be continuous.

What not to copy:

- Its closed BUD protocol.
- The game-streaming video pipeline.

### Sunshine / Moonlight

Why it matters:

- Shows how much product polish low-latency interactive streaming needs: device pairing, host configuration, hardware acceleration, client diversity.

What LwyLink should learn:

- Real-time media needs good diagnostics and hardware/device assumptions.
- Local-first setup and pairing UX are worth studying for room creation/join flows.

What not to copy:

- Video streaming architecture.
- Heavy GPU-oriented assumptions.

### libp2p

Why it matters:

- Its NAT traversal stack separates relay, direct connection upgrade, and AutoNAT-like reachability detection.

What LwyLink should learn:

- Relay can be an explicit transport used to coordinate a direct upgrade.
- Reachability diagnostics should be a first-class module.
- QUIC is worth evaluating for future relay paths.

What not to copy:

- DHT, gossip, and global peer routing. LwyLink has rooms and a control server, so it does not need a global decentralized network.

## LwyLink Module Impact

### Control Service

Should evolve toward:

- Room membership authority.
- Signaling authority.
- Candidate/diagnostic exchange.
- Release/account/admin surface.

Should avoid:

- Carrying high-rate media.
- Becoming a full distributed routing system.

### Node Service

Should evolve toward:

- TCP relay for reliability.
- UDP rendezvous.
- Candidate observation.
- Future UDP/QUIC relay for application traffic if TCP head-of-line blocking becomes a practical problem.

Should avoid:

- Media mixing for now.
- Replacing TURN for WebRTC voice unless product evidence shows a clear need.

### Windows Client

Should evolve toward:

- Clear path diagnostics.
- Separate virtual LAN, voice, and diagnostics modules.
- WebRTC voice as the single product media engine.

Should avoid:

- More hidden audio knobs.
- Treating fixed jitter as the main quality solution.

### Voice Module

Current Windows voice should be treated as:

- WebRTC media for the product client.
- LwyLink room membership, mic limits, and signaling around WebRTC.
- TURN still required before calling the voice path complete for restrictive NATs.

The native overlay voice frontend prototype and low-level overlay voice frame path have been removed to avoid carrying two media engines. The overlay product boundary is now virtual LAN IP traffic; room voice belongs to WebRTC.

## Voice Route Decision

The WebRTC experiment is complete enough for the Windows product path. Continue with WebRTC voice unless one of these product-level problems appears:

- WebView2 cannot reliably run WebRTC audio capture/playback.
- TURN configuration or credentials are too fragile for self-hosted users.
- `getStats()` does not provide enough path/quality visibility in Wails.
- Latency or audio quality regresses below the removed native overlay prototype.

Keep native overlay voice out of the Windows product path because:

- WebRTC clearly beats the removed prototype in voice quality and latency.
- Implementing PLC/adaptive jitter starts duplicating too much WebRTC/NetEQ functionality.
- TCP relay head-of-line blocking dominates and would require a new media relay anyway.

## Decision Bias

The bias should be:

- Use LwyLink overlay where LwyLink is unique: virtual LAN, room networking, P2P/relay for arbitrary app traffic.
- Use mature media systems where the problem is already solved: voice capture, jitter, echo, loss concealment, media NAT traversal.

This is now the default product direction. Future experiments should prove a concrete improvement before adding another client media engine.
