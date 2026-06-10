export const webRTCHighDirectRTTMS = 350;
export const webRTCHighDirectSampleLimit = 3;
export const webRTCSwitchMarginMS = 30;

export function hasRelayICEServer(servers: RTCIceServer[]) {
  return servers.some((server) => iceURLs(server.urls).some((url) => /^turns?:/i.test(url.trim())));
}

export function isDirectCandidateType(type: string) {
  return type === "host" || type === "srflx" || type === "prflx";
}

export function nextVoiceRelayFallbackState(input: {
  candidateType: string;
  roundTripTimeMS: number;
  currentSamples: number;
  relayAvailable: boolean;
  relayForced: boolean;
}) {
  if (input.relayForced || !input.relayAvailable || !isDirectCandidateType(input.candidateType) || input.roundTripTimeMS < webRTCHighDirectRTTMS) {
    return { samples: 0, shouldFallback: false };
  }
  const samples = input.currentSamples + 1;
  return { samples, shouldFallback: samples >= webRTCHighDirectSampleLimit };
}

export function nextVoiceDirectRetryState(input: {
  candidateType: string;
  roundTripTimeMS: number;
  directRTTAtFallbackMS: number;
  currentSamples: number;
  relayForced: boolean;
}) {
  if (
    !input.relayForced ||
    input.candidateType !== "relay" ||
    input.directRTTAtFallbackMS <= 0 ||
    input.roundTripTimeMS <= input.directRTTAtFallbackMS + webRTCSwitchMarginMS
  ) {
    return { samples: 0, shouldRetryDirect: false };
  }
  const samples = input.currentSamples + 1;
  return { samples, shouldRetryDirect: samples >= webRTCHighDirectSampleLimit };
}

function iceURLs(urls: RTCIceServer["urls"]) {
  if (Array.isArray(urls)) {
    return urls.filter((url): url is string => typeof url === "string");
  }
  return typeof urls === "string" ? [urls] : [];
}
