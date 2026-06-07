#!/usr/bin/env python3
"""Example Python client for LwyLink room APIs and SSE room events."""

from __future__ import annotations

import argparse
import json
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any


DEFAULT_API = "http://127.0.0.1:8080"


@dataclass
class LwyLinkRoomClient:
    api: str
    token: str = ""

    def request(self, method: str, path: str, body: Any | None = None) -> Any:
        data = None
        headers = {"Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        if body is not None:
            data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        req = urllib.request.Request(self.api.rstrip("/") + path, data=data, method=method, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=20) as resp:
                raw = resp.read().decode("utf-8")
        except urllib.error.HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="replace")
            try:
                message = json.loads(raw).get("error") or raw
            except json.JSONDecodeError:
                message = raw
            raise RuntimeError(f"http {exc.code}: {message}") from exc
        return json.loads(raw) if raw.strip() else None

    def register(self, username: str, password: str, display_name: str = "") -> dict[str, Any]:
        result = self.request("POST", "/api/register", {"username": username, "password": password, "displayName": display_name})
        self.token = result["token"]
        return result

    def login(self, username: str, password: str) -> dict[str, Any]:
        result = self.request("POST", "/api/login", {"username": username, "password": password})
        self.token = result["token"]
        return result

    def me(self) -> dict[str, Any]:
        return self.request("GET", "/api/me")

    def rooms(self) -> list[dict[str, Any]]:
        return self.request("GET", "/api/rooms")

    def create_room(self, name: str, max_members: int) -> dict[str, Any]:
        return self.request("POST", "/api/rooms", {"name": name, "maxMembers": max_members})

    def join_room(self, room_public_key: str) -> dict[str, Any]:
        return self.request("POST", "/api/rooms/join", {"roomPublicKey": room_public_key})

    def members(self, room_id: int) -> list[dict[str, Any]]:
        query = urllib.parse.urlencode({"roomID": room_id})
        return self.request("GET", "/api/rooms/members?" + query)

    def send_chat(self, room_id: int, message: str) -> dict[str, Any]:
        return self.request("POST", "/api/rooms/chat", {"roomID": room_id, "message": message})

    def leave_room(self, room_id: int) -> Any:
        return self.request("POST", "/api/rooms/leave", {"roomID": room_id})

    def delete_room(self, room_id: int) -> Any:
        return self.request("POST", "/api/rooms/delete", {"roomID": room_id})

    def room_events(self):
        headers = {}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        req = urllib.request.Request(self.api.rstrip() + "/api/rooms/events", method="GET", headers=headers)
        with urllib.request.urlopen(req, timeout=None) as resp:
            event_data: list[str] = []
            for raw in resp:
                line = raw.decode("utf-8").rstrip("\r\n")
                if line == "":
                    if event_data:
                        yield json.loads("\n".join(event_data))
                        event_data.clear()
                    continue
                if line.startswith("data:"):
                    event_data.append(line.removeprefix("data:").strip())


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    client = LwyLinkRoomClient(args.api, args.token)

    if args.command == "register":
        print_json(client.register(args.username, args.password, args.display_name))
    elif args.command == "login":
        print_json(client.login(args.username, args.password))
    elif args.command == "me":
        print_json(client.me())
    elif args.command == "rooms":
        print_json(client.rooms())
    elif args.command == "create-room":
        print_json(client.create_room(args.name, args.max_members))
    elif args.command == "join-room":
        print_json(client.join_room(args.room_public_key))
    elif args.command == "members":
        print_json(client.members(args.room_id))
    elif args.command == "chat":
        print_json(client.send_chat(args.room_id, args.message))
    elif args.command == "leave-room":
        print_json(client.leave_room(args.room_id))
    elif args.command == "delete-room":
        print_json(client.delete_room(args.room_id))
    elif args.command == "events":
        for event in client.room_events():
            print_json(event)
    else:
        parser.print_help()
        return 2
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Python example for LwyLink room APIs")
    parser.add_argument("--api", default=DEFAULT_API)
    parser.add_argument("--token", default="")
    parser.add_argument("--self-test", action="store_true")
    sub = parser.add_subparsers(dest="command")

    register = sub.add_parser("register")
    register.add_argument("--username", required=True)
    register.add_argument("--password", required=True)
    register.add_argument("--display-name", default="")

    login = sub.add_parser("login")
    login.add_argument("--username", required=True)
    login.add_argument("--password", required=True)

    sub.add_parser("me")
    sub.add_parser("rooms")

    create = sub.add_parser("create-room")
    create.add_argument("--name", required=True)
    create.add_argument("--max-members", type=int, default=8)

    join = sub.add_parser("join-room")
    join.add_argument("--room-public-key", required=True)

    members = sub.add_parser("members")
    members.add_argument("--room-id", type=int, required=True)

    chat = sub.add_parser("chat")
    chat.add_argument("--room-id", type=int, required=True)
    chat.add_argument("--message", required=True)

    leave = sub.add_parser("leave-room")
    leave.add_argument("--room-id", type=int, required=True)

    delete = sub.add_parser("delete-room")
    delete.add_argument("--room-id", type=int, required=True)

    sub.add_parser("events")
    return parser


def self_test() -> int:
    sample = b'event: room\ndata: {"type":"connected","roomID":1}\n\n'
    parsed = []
    data: list[str] = []
    for raw in sample.splitlines():
        line = raw.decode("utf-8")
        if line == "":
            if data:
                parsed.append(json.loads("\n".join(data)))
                data.clear()
            continue
        if line.startswith("data:"):
            data.append(line.removeprefix("data:").strip())
    if parsed != [{"type": "connected", "roomID": 1}]:
        print("self-test failed to parse SSE sample", file=sys.stderr)
        return 1
    print("self-test ok: room API helpers and SSE parser are ready")
    return 0


def print_json(value: Any) -> None:
    print(json.dumps(value, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    raise SystemExit(main())
