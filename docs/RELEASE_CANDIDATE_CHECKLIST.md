# LwyLink Stable Candidate Checklist

This checklist is for promotion-ready builds. It separates automated checks from
real network checks so a release does not rely on a single developer machine.

## Automated Baseline

Run before every stable candidate:

```powershell
PowerShell -ExecutionPolicy Bypass -File .\deploy\build-all.ps1 -Vet
PowerShell -ExecutionPolicy Bypass -File .\deploy\check.ps1 -Vet
```

After deployment, run the post-release check against the public control API and node:

```powershell
PowerShell -ExecutionPolicy Bypass -File .\deploy\post-release-check.ps1
```

This command expects `configs/deploy.local.json` or explicit `-HostName` /
`-OverlayAddr` values. Use `-SkipCoturn` or `-SkipBackupTimer` only for
environments where those components are intentionally not installed.

The release is not a candidate unless healthz, latest release, smoke, coturn,
and backup timer checks pass.

## Network Matrix

Test with two Windows clients on different accounts. Record the result for each
pair: overlay status, player acceleration label, voice label, and whether traffic
is counted as relay.

| Case | Client A | Client B | Expected Product Behavior |
| --- | --- | --- | --- |
| LAN | Same router | Same router | Overlay connects, acceleration should usually be available, voice should prefer P2P. |
| Home + Home | Normal home broadband | Normal home broadband | TCP relay works immediately; UDP acceleration should often recover. |
| Home + Mobile | Home broadband | Phone hotspot | TCP relay works; UDP acceleration may or may not work. |
| Home + CGN | Home broadband | Carrier CGNAT | TCP relay works; UDP acceleration is opportunistic only. |
| CGN + CGN | Carrier CGNAT | Carrier CGNAT | TCP relay must work; do not promise direct acceleration. |
| UDP Blocked | Firewall blocks UDP | Any network | TCP relay works, UI stays calm, no repeated path flapping. |
| TURN Voice | Force WebRTC relay candidate | Any network | Voice works through TURN and relay traffic quota increases. |

## Manual Acceptance

- Room creation and join page should look compact and clear.
- Room key input should not occupy a large multi-line block.
- Room page should show room info, combined voice/text communication, and a
  two-column member list on wide windows.
- Member rows should show product-level path labels such as `支持加速`,
  `加速检测`, or `自动中继`, not peer names or low-level endpoint details.
- Chat input sends with Enter and stays single-line.
- Voice join requires the virtual network and shows a clear prompt when needed.
- Closing the virtual network also leaves local voice.
- Admin metrics page should avoid duplicate summary numbers and show useful
  operational signals: overlay connections, voice usage, download trend, relay
  traffic, latest release, and node errors.
- Python and Java examples are virtual-network integration examples only; they
  should not include WebRTC voice.

## What To Capture

For every failed or suspicious case, capture:

- Client screenshots of the room page and settings page.
- Backend admin metrics screenshot.
- `lwylink-net.exe` JSON transport events if using the standalone engine.
- Whether the player list says `支持加速`, `加速检测`, or `自动中继`.
- Whether the voice label says `P2P 语音`, `TURN 语音`, `高清语音`, or reconnecting.
- Approximate time from entering the room to stable connectivity.

## Release Rule

Ship the stable candidate only when:

- Automated baseline passes.
- Public smoke passes.
- At least LAN, Home + Home, Home + Mobile, and UDP Blocked have been manually
  checked.
- CGN cases fall back cleanly to relay without repeated switching.
- Any known issue is documented in release notes or fixed before upload.
