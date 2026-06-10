import assert from "node:assert/strict";
import test from "node:test";
import {
  hasRelayICEServer,
  nextVoiceDirectRetryState,
  nextVoiceRelayFallbackState,
  webRTCHighDirectSampleLimit,
} from "../test-dist/webrtcPathPolicy.js";

test("hasRelayICEServer detects TURN while ignoring STUN", () => {
  assert.equal(hasRelayICEServer([{ urls: "stun:stun.example.com:3478" }]), false);
  assert.equal(hasRelayICEServer([{ urls: ["stun:stun.example.com:3478", "turns:turn.example.com:5349"] }]), true);
});

test("nextVoiceRelayFallbackState requires consecutive high direct RTT", () => {
  let state = { samples: 0, shouldFallback: false };
  for (let i = 0; i < webRTCHighDirectSampleLimit - 1; i++) {
    state = nextVoiceRelayFallbackState({
      candidateType: "srflx",
      roundTripTimeMS: 430,
      currentSamples: state.samples,
      relayAvailable: true,
      relayForced: false,
    });
    assert.equal(state.shouldFallback, false);
  }
  state = nextVoiceRelayFallbackState({
    candidateType: "srflx",
    roundTripTimeMS: 430,
    currentSamples: state.samples,
    relayAvailable: true,
    relayForced: false,
  });
  assert.equal(state.shouldFallback, true);
});

test("nextVoiceRelayFallbackState resets when relay is unavailable or path is already relay", () => {
  assert.deepEqual(
    nextVoiceRelayFallbackState({
      candidateType: "relay",
      roundTripTimeMS: 600,
      currentSamples: 2,
      relayAvailable: true,
      relayForced: false,
    }),
    { samples: 0, shouldFallback: false },
  );
  assert.deepEqual(
    nextVoiceRelayFallbackState({
      candidateType: "host",
      roundTripTimeMS: 600,
      currentSamples: 2,
      relayAvailable: false,
      relayForced: false,
    }),
    { samples: 0, shouldFallback: false },
  );
});

test("nextVoiceDirectRetryState retries direct only when forced relay is worse", () => {
  let state = { samples: 0, shouldRetryDirect: false };
  for (let i = 0; i < webRTCHighDirectSampleLimit - 1; i++) {
    state = nextVoiceDirectRetryState({
      candidateType: "relay",
      roundTripTimeMS: 520,
      directRTTAtFallbackMS: 430,
      currentSamples: state.samples,
      relayForced: true,
    });
    assert.equal(state.shouldRetryDirect, false);
  }
  state = nextVoiceDirectRetryState({
    candidateType: "relay",
    roundTripTimeMS: 520,
    directRTTAtFallbackMS: 430,
    currentSamples: state.samples,
    relayForced: true,
  });
  assert.equal(state.shouldRetryDirect, true);
});

test("nextVoiceDirectRetryState resets when relay is still better", () => {
  assert.deepEqual(
    nextVoiceDirectRetryState({
      candidateType: "relay",
      roundTripTimeMS: 440,
      directRTTAtFallbackMS: 430,
      currentSamples: 2,
      relayForced: true,
    }),
    { samples: 0, shouldRetryDirect: false },
  );
});
