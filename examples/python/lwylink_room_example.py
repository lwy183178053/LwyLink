#!/usr/bin/env python3
"""CLI example that calls the copy-paste friendly lwylink.py library."""

from __future__ import annotations

import argparse
import json
import sys
from typing import Any

from lwylink import DEFAULT_API, RoomClient


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    client = RoomClient(args.api, args.token)

    if args.command == "register":
        print_json(client.register(args.email, args.password, args.display_name, args.email_code))
    elif args.command == "send-code":
        print_json(client.send_email_verification(args.email, args.purpose))
    elif args.command == "reset-password":
        print_json(client.reset_password(args.email, args.email_code, args.new_password))
    elif args.command == "change-password":
        print_json(client.change_password(args.old_password, args.new_password))
    elif args.command == "login":
        print_json(client.login(args.email, args.password))
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
    register.add_argument("--email", required=True)
    register.add_argument("--password", required=True)
    register.add_argument("--display-name", default="")
    register.add_argument("--email-code", default="")

    send_code = sub.add_parser("send-code")
    send_code.add_argument("--email", required=True)
    send_code.add_argument("--purpose", choices=["register", "password_reset"], default="register")

    reset = sub.add_parser("reset-password")
    reset.add_argument("--email", required=True)
    reset.add_argument("--email-code", required=True)
    reset.add_argument("--new-password", required=True)

    change = sub.add_parser("change-password")
    change.add_argument("--old-password", required=True)
    change.add_argument("--new-password", required=True)

    login = sub.add_parser("login")
    login.add_argument("--email", required=True)
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
    print("self-test ok: room API library is ready")
    return 0


def print_json(value: Any) -> None:
    print(json.dumps(value, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    raise SystemExit(main())
