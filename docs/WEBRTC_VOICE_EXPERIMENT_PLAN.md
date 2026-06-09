# LwyLink WebRTC Voice Decision Record

Status: archived decision record. The Windows client voice product path is now WebRTC media with LwyLink room membership, signaling, and mic controls around it. The former native overlay voice frontend module and low-level `FrameVoice / UDPTypeVoice` transport path have been removed.

The sections below are kept as rationale for why the project moved away from maintaining two client media engines.

## Why This Decision Exists

The former native overlay voice path proved that voice could share LwyLink room membership, P2P/relay routing, and traffic accounting. However, real-time voice quality depends on a full media stack:

- Adaptive jitter buffering.
- Packet loss concealment.
- Echo cancellation.
- Noise suppression.
- Automatic gain.
- ICE/STUN/TURN candidate selection.
- Audio device compatibility.
- Per-peer stats and network quality adaptation.

WebRTC already ships these capabilities. The experiment answered one question:

> Should LwyLink voice use WebRTC media while LwyLink overlay remains responsible for virtual LAN traffic?

## Current LwyLink Advantages

Existing code already provides most of the non-media foundation:

- Account authentication.
- Room membership and owner controls.
- Voice room join/leave/mic state.
- Room SSE events at `/api/rooms/events`.
- Windows frontend audio settings UI.
- P2P/relay state concepts in the client.

This meant the WebRTC experiment did not need a new product surface. It mainly needed signaling and peer connection management.

## Minimal Architecture

```text
Windows Client A
  getUserMedia + RTCPeerConnection
  offer/answer/ice over HTTPS API

Control Server
  authenticates room members
  stores or forwards signaling events
  emits signaling through existing room SSE stream

Windows Client B
  getUserMedia + RTCPeerConnection
  receives offer/answer/ice through room events
```

Virtual LAN packets stay on the existing LwyLink overlay. Only room voice changes.

## Server-Side Shape

Reuse the existing room event stream instead of adding WebSocket first.

Candidate API:

- `POST /api/rooms/voice/webrtc/signal`

Request:

```json
{
  "roomID": 123,
  "toUserID": 456,
  "type": "offer|answer|ice|hangup",
  "payload": {}
}
```

Validation:

- Caller must be logged in.
- Caller must be in the room.
- Caller must have joined voice.
- Target user must be in the same room.
- Target user must have joined voice.
- Payload size must be capped.
- Signal should expire quickly and should not be persisted long term.

Delivery:

- Add a room event type such as `voice-webrtc-signal`.
- Push it to the target user's existing SSE subscription.

Why SSE was acceptable for the experiment:

- Signaling is sparse.
- Media does not flow through SSE.
- The current app already maintains the event stream for room state.

When to move to WebSocket:

- If trickle ICE through SSE has reliability issues.
- If bidirectional signaling latency becomes visible.
- If mobile clients are added later.

## Client-Side Shape Chosen

The Windows client now keeps WebRTC voice in:

- `cmd/lwylink-windows-ui/frontend/src/webrtcVoice.ts`

Responsibilities:

- Create one `RTCPeerConnection` per remote voice participant.
- Use `navigator.mediaDevices.getUserMedia`.
- Add local audio track only when mic is enabled.
- Exchange offer/answer/ICE through the new signaling API and SSE events.
- Render remote audio through regular `HTMLAudioElement` or Web Audio graph.
- Periodically call `getStats()`.

The experiment phase is over for the Windows client. The native voice frontend module has been removed; keep this document as rationale and historical context, not as an active implementation plan.

## ICE Servers

Development started with:

- Public STUN for quick validation.
- Configurable STUN/TURN list from server config.

Production still needs:

- Self-hosted coturn or equivalent TURN service.
- TURN credentials generated per session or short TTL.
- TURN traffic accounting if relay cost matters.

## Stats To Collect

For each peer connection:

- selected candidate pair.
- local candidate type: `host`, `srflx`, `relay`.
- remote candidate type.
- currentRoundTripTime.
- jitter.
- packetsLost.
- packetsSent/packetsReceived.
- bytesSent/bytesReceived.
- audioLevel if available.
- totalAudioEnergy if available.

These stats should be shown in dev logs or a hidden diagnostic panel before they become user-facing UI.

## Original A/B Test Matrix

| Scenario | Native Overlay Voice | WebRTC Voice |
| --- | --- | --- |
| same LAN | required | required |
| normal home NAT to home NAT | required | required |
| UDP direct succeeds | required | required |
| UDP direct fails, relay path | required | TURN required |
| packet jitter | required | required |
| two speakers | required | required |
| small voice / tail | required | required |

## Original Pass Criteria

WebRTC was promoted to the main voice route because it proved:

- Lower perceived delay than native fixed-buffer voice.
- Better small voice and tail handling.
- Fewer audible discontinuities.
- Good path observability through `getStats()`.
- Acceptable TURN traffic cost.
- No unacceptable Wails/WebView2 compatibility issue.

Native overlay voice would have stayed the likely main route only if:

- WebRTC is blocked or unstable in Wails/WebView2.
- TURN deployment/cost is unacceptable.
- WebRTC signaling complexity becomes larger than expected.
- Native overlay with RTP-like fixes matches or beats WebRTC in tests.

## Risks

- Browser WebRTC implementation in WebView2 may behave differently from Chrome.
- TURN cost may be higher than TCP relay for the same usage.
- Maintaining native voice and WebRTC voice together can increase complexity.
- WebRTC permissions and device switching need careful UI handling.

## Current Product Status After 0.2.4

The first backend skeleton has shipped:

- `RoomEventVoiceSignal`
- `VoiceSignal`
- `Service.SendVoiceSignal`
- `POST /api/rooms/voice/webrtc/signal`

The skeleton authenticates and forwards signaling through the existing room event bus. It does not carry media itself.

The frontend now includes:

- `webrtcVoice.ts`
- Wails binding `SendVoiceWebRTCSignal`
- Wails event `voice-webrtc-signal`
- server-provided ICE configuration through `/api/rooms/voice/webrtc/ice`

The Windows client voice backend is now WebRTC only. The former native overlay voice frontend module, hidden backend selector, and dual-audio diagnostic mode were removed after listening tests confirmed WebRTC quality was acceptable. This avoids maintaining two media engines and prevents echo/double-audio confusion.

The lower-level `FrameVoice / UDPTypeVoice` protocol path has also been removed, so the overlay now focuses on virtual LAN IP traffic while voice stays on WebRTC.

WebRTC local audio tracks start muted. They are enabled only after the normal room mic button calls `SetRoomVoiceMic`, so the same voice-room semantics and mic limits are kept.

The WebRTC peer lifecycle now handles two common real-world issues:

- Early ICE candidates are buffered until the remote SDP is applied.
- Short `disconnected` states use a grace window before rebuilding the peer connection.

Latest WebRTC stats are exposed for manual inspection:

```js
window.__lwylinkWebRTCVoiceStats
```

Known gaps after promotion to default:

- There must be a deployed TURN server before restrictive NAT voice can be considered complete.
- ICE servers are server-provided; production should use a controlled LwyLink STUN/TURN service instead of depending on public STUN.
- TURN REST short-lived credentials are supported in config, but coturn is not deployed yet.
- WebRTC stats are exposed through a debug global and a compact voice UI indicator.
- Dynamic voice membership and reconnection need to be verified with real multi-client tests.
- TURN relay traffic is now integrated through client-reported WebRTC stats when the selected candidate type is `relay`; a future coturn-side accounting hook would be more authoritative.

Next engineering step:

- Deploy coturn or an equivalent TURN service.
- Configure coturn `use-auth-secret/static-auth-secret` and LwyLink `voiceTURNCredential`.
- Expand WebRTC diagnostics into a dedicated detail view if compact UI data is not enough.
- Verify dynamic join/leave and reconnect behavior with two or more real clients.
- Keep deleting dead client code when a WebRTC/TURN path replaces an old experiment.

## Manual WebRTC Voice Checks

When testing a new voice build, record both subjective audio quality and the compact UI indicator:

- Path label: `P2P 语音`, `TURN 语音`, `高清语音`, or `语音连接中`.
- RTT: stable low values usually mean the selected candidate pair is healthy.
- Jitter: spikes correlate with choppy audio or robotic transitions.
- Packet loss: repeated increases mean the issue is likely network path quality, not microphone gain.

The debug global is still available for deeper inspection:

```js
window.__lwylinkWebRTCVoiceStats
```

## What Not To Do

- Do not claim WebRTC voice is complete before TURN is tested.
- Do not hide WebRTC path type from diagnostics.
- Do not treat “connects once” as success; voice quality is the real metric.
