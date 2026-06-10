import assert from "node:assert/strict";
import test from "node:test";
import {
  appendDedupedDataChatMessage,
  buildDataChatPayload,
  chatSignalType,
  decodeDataChatMessage,
  isCurrentDataChatMessage,
  maxDataChatHistoryItems,
  maxDataChatMessageRunes,
  shouldFallbackToServerChat,
} from "../test-dist/webrtcChatLogic.js";

test("buildDataChatPayload trims text and keeps sender metadata", () => {
  const now = new Date("2026-06-10T06:00:00.000Z");
  const payload = buildDataChatPayload({
    roomID: 42,
    selfUserID: 7,
    message: "  hello p2p  ",
    sender: { username: "player@example.com", displayName: "Player" },
    now,
    randomID: "abc123",
  });

  assert.deepEqual(payload, {
    id: "p2p-7-1781071200000-abc123",
    roomID: 42,
    userID: 7,
    username: "player@example.com",
    displayName: "Player",
    message: "hello p2p",
    createdAt: "2026-06-10T06:00:00.000Z",
  });
});

test("buildDataChatPayload rejects blank and overlong messages", () => {
  assert.equal(buildDataChatPayload({ roomID: 1, selfUserID: 2, message: "   " }), undefined);
  assert.equal(buildDataChatPayload({ roomID: 1, selfUserID: 2, message: "测".repeat(maxDataChatMessageRunes + 1) }), undefined);
});

test("decodeDataChatMessage accepts only the expected room and peer", () => {
  const encoded = JSON.stringify({
    id: "p2p-9-fixed",
    roomID: 100,
    userID: 9,
    username: "peer@example.com",
    displayName: "Peer",
    message: "  from datachannel ",
    createdAt: "2026-06-10T07:00:00.000Z",
  });

  assert.deepEqual(decodeDataChatMessage(encoded, 100, 9), {
    id: "p2p-9-fixed",
    roomID: 100,
    userID: 9,
    username: "peer@example.com",
    displayName: "Peer",
    message: "from datachannel",
    createdAt: "2026-06-10T07:00:00.000Z",
  });
  assert.equal(decodeDataChatMessage(encoded, 101, 9), undefined);
  assert.equal(decodeDataChatMessage(encoded, 100, 10), undefined);
});

test("decodeDataChatMessage rejects malformed and oversized payloads", () => {
  assert.equal(decodeDataChatMessage("not-json", 1, 2), undefined);
  assert.equal(decodeDataChatMessage(JSON.stringify({ roomID: 1, userID: 2, message: "" }), 1, 2), undefined);
  assert.equal(decodeDataChatMessage("x".repeat(4097), 1, 2), undefined);
});

test("chatSignalType only allows chat signaling types", () => {
  assert.equal(chatSignalType("chat-offer"), "offer");
  assert.equal(chatSignalType("chat-answer"), "answer");
  assert.equal(chatSignalType("chat-ice"), "ice");
  assert.equal(chatSignalType("chat-hangup"), "hangup");
  assert.equal(chatSignalType("offer"), "");
  assert.equal(chatSignalType("chat-reset"), "");
});

test("shouldFallbackToServerChat falls back only when P2P send fails", () => {
  assert.equal(shouldFallbackToServerChat({ currentRoomID: 9, message: " hello ", p2pSent: false }), true);
  assert.equal(shouldFallbackToServerChat({ currentRoomID: 9, message: " hello ", p2pSent: true }), false);
  assert.equal(shouldFallbackToServerChat({ currentRoomID: 9, message: "   ", p2pSent: false }), false);
  assert.equal(shouldFallbackToServerChat({ currentRoomID: undefined, message: "hello", p2pSent: false }), false);
});

test("appendDedupedDataChatMessage skips duplicate message ids", () => {
  const first = dataMessage({ id: "p2p-1", message: "first" });
  const duplicate = dataMessage({ id: "p2p-1", message: "duplicate" });
  const second = dataMessage({ id: "p2p-2", message: "second" });

  const withFirst = appendDedupedDataChatMessage([], first);
  const withDuplicate = appendDedupedDataChatMessage(withFirst, duplicate);
  const withSecond = appendDedupedDataChatMessage(withDuplicate, second);

  assert.deepEqual(withDuplicate, withFirst);
  assert.deepEqual(withSecond.map((item) => item.message), ["first", "second"]);
});

test("appendDedupedDataChatMessage keeps the latest bounded history", () => {
  const items = Array.from({ length: maxDataChatHistoryItems }, (_, index) =>
    dataMessage({ id: `p2p-${index + 1}`, message: `message ${index + 1}` }),
  );
  const next = appendDedupedDataChatMessage(items, dataMessage({ id: "p2p-new", message: "new" }));

  assert.equal(next.length, maxDataChatHistoryItems);
  assert.equal(next[0].id, "p2p-2");
  assert.equal(next.at(-1)?.id, "p2p-new");
});

test("isCurrentDataChatMessage isolates stale room and session messages", () => {
  const message = dataMessage({ roomID: 42 });

  assert.equal(isCurrentDataChatMessage({ message, currentRoomID: 42, sessionRoomID: 42 }), true);
  assert.equal(isCurrentDataChatMessage({ message, currentRoomID: 43, sessionRoomID: 43 }), false);
  assert.equal(isCurrentDataChatMessage({ message, currentRoomID: 42, sessionRoomID: 41 }), false);
  assert.equal(isCurrentDataChatMessage({ message, currentRoomID: undefined, sessionRoomID: 42 }), false);
});

function dataMessage(overrides = {}) {
  return {
    id: "p2p-fixed",
    roomID: 42,
    userID: 7,
    username: "player@example.com",
    displayName: "Player",
    message: "hello",
    createdAt: "2026-06-10T08:00:00.000Z",
    ...overrides,
  };
}
