# LwyLink WebRTC Voice Experiment Plan

This is an experiment plan, not a release plan. Do not publish a client version from this work until the experiment beats the current native overlay voice path with evidence.

## Why This Experiment Exists

The current native overlay voice path proves that voice can share LwyLink room membership, P2P/relay routing, and traffic accounting. However, real-time voice quality depends on a full media stack:

- Adaptive jitter buffering.
- Packet loss concealment.
- Echo cancellation.
- Noise suppression.
- Automatic gain.
- ICE/STUN/TURN candidate selection.
- Audio device compatibility.
- Per-peer stats and network quality adaptation.

WebRTC already ships these capabilities. The experiment should answer one question:

> Should LwyLink voice use WebRTC media while LwyLink overlay remains responsible for virtual LAN traffic?

## Current LwyLink Advantages

Existing code already provides most of the non-media foundation:

- Account authentication.
- Room membership and owner controls.
- Voice room join/leave/mic state.
- Room SSE events at `/api/rooms/events`.
- Windows frontend audio settings UI.
- P2P/relay state concepts in the client.

This means the WebRTC experiment does not need a new product surface. It mainly needs signaling and peer connection management.

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

Why SSE is acceptable for the experiment:

- Signaling is sparse.
- Media does not flow through SSE.
- The current app already maintains the event stream for room state.

When to move to WebSocket:

- If trickle ICE through SSE has reliability issues.
- If bidirectional signaling latency becomes visible.
- If mobile clients are added later.

## Client-Side Shape

Create a separate experimental module, for example:

- `cmd/lwylink-windows-ui/frontend/src/webrtcVoice.ts`

Responsibilities:

- Create one `RTCPeerConnection` per remote voice participant.
- Use `navigator.mediaDevices.getUserMedia`.
- Add local audio track only when mic is enabled.
- Exchange offer/answer/ICE through the new signaling API and SSE events.
- Render remote audio through regular `HTMLAudioElement` or Web Audio graph.
- Periodically call `getStats()`.

Do not remove the current native voice module during the experiment.

## ICE Servers

Start with:

- Public STUN for quick experiment.
- Configurable STUN/TURN list from server config.

Production likely needs:

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

## A/B Test Matrix

| Scenario | Native Overlay Voice | WebRTC Voice |
| --- | --- | --- |
| same LAN | required | required |
| normal home NAT to home NAT | required | required |
| UDP direct succeeds | required | required |
| UDP direct fails, relay path | required | TURN required |
| packet jitter | required | required |
| two speakers | required | required |
| small voice / tail | required | required |

## Pass Criteria

WebRTC becomes the likely main voice route if it proves:

- Lower perceived delay than native fixed-buffer voice.
- Better small voice and tail handling.
- Fewer audible discontinuities.
- Good path observability through `getStats()`.
- Acceptable TURN traffic cost.
- No unacceptable Wails/WebView2 compatibility issue.

Native overlay voice stays the likely main route if:

- WebRTC is blocked or unstable in Wails/WebView2.
- TURN deployment/cost is unacceptable.
- WebRTC signaling complexity becomes larger than expected.
- Native overlay with RTP-like fixes matches or beats WebRTC in tests.

## Risks

- Browser WebRTC implementation in WebView2 may behave differently from Chrome.
- TURN cost may be higher than TCP relay for the same usage.
- Maintaining native voice and WebRTC voice together can increase complexity.
- WebRTC permissions and device switching need careful UI handling.

## Recommended Branch Strategy

Use an experiment branch:

```powershell
git switch -c experiment/webrtc-voice
```

Keep commits small:

1. Server signaling API and room event type.
2. Frontend WebRTC voice module with fake/manual signaling tests.
3. Client integration behind a local feature flag.
4. Stats panel.
5. A/B test artifacts.

Do not publish a forced update from this branch until the experiment clearly wins.

## Current Local Experiment Status

The first backend skeleton has been drafted locally:

- `RoomEventVoiceSignal`
- `VoiceSignal`
- `Service.SendVoiceSignal`
- `POST /api/rooms/voice/webrtc/signal`

The skeleton only authenticates and forwards signaling through the existing room event bus. It does not change the current native overlay voice path and does not carry media.

The first frontend skeleton has also been drafted locally:

- `webrtcVoice.ts`
- Wails binding `SendVoiceWebRTCSignal`
- Wails event `voice-webrtc-signal`
- hidden backend selector: `localStorage.setItem("lwylink.voiceBackend", "webrtc")`

The default backend is `native`, so normal users continue using the existing native overlay voice path.

WebRTC local audio tracks start muted. They are enabled only after the normal room mic button calls `SetRoomVoiceMic`, so the experiment keeps the same voice-room semantics as native overlay voice.

Latest WebRTC stats are exposed for manual inspection:

```js
window.__lwylinkWebRTCVoiceStats
```

Local backend modes:

- `native`: current LwyLink overlay voice only.
- `webrtc`: WebRTC voice only.
- `dual`: native overlay voice and WebRTC voice in parallel, useful only for diagnostics because it can cause echo/double audio.

Examples:

```js
localStorage.setItem("lwylink.voiceBackend", "native")
localStorage.setItem("lwylink.voiceBackend", "webrtc")
localStorage.setItem("lwylink.voiceBackend", "dual")
localStorage.removeItem("lwylink.voiceBackend")
```

The legacy flag `lwylink.webrtcVoiceExperiment=1` still maps to `dual` for convenience, but new tests should use `lwylink.voiceBackend`.

Next experiment step:

- Build and run two local clients with the hidden flag enabled.
- Exchange offer/answer/ICE between two clients through the existing room SSE stream.
- Collect `RTCPeerConnection.getStats()` for candidate type, RTT, jitter, and packet loss.
- Compare WebRTC audio with native overlay audio before deciding the product route.

Recommended A/B order:

1. Run both clients in `native`, record current baseline.
2. Run both clients in `webrtc`, test small voice, tail, latency, and stats.
3. Use `dual` only to confirm signaling and peer path behavior; do not use it for final listening judgment.

## What Not To Do

- Do not delete native overlay voice during the experiment.
- Do not ship WebRTC as default before TURN is tested.
- Do not hide WebRTC path type from diagnostics.
- Do not treat “connects once” as success; voice quality is the real metric.
