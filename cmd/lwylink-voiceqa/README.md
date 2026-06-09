# lwylink-voiceqa

`lwylink-voiceqa` is an automated voice transport QA tool. It creates two temporary users, creates one room, connects two overlay clients, sends deterministic PCM16 voice frames in both directions, and compares sent/received audio payloads.

Example:

```powershell
$env:LWYLINK_ADMIN_TOKEN = "<admin-token>"
go run ./cmd/lwylink-voiceqa `
  -api https://www.lwylink.xyz `
  -overlay 64.83.34.53:35768 `
  -frames 180 `
  -runs 10 `
  -jitter-ms 800 `
  -out build/voiceqa-auto
```

The report includes payload hashes, frame counts, gap spikes, and simulated playback underruns. A failed run stops immediately and keeps `report.json` plus sent/received WAV files for comparison.
