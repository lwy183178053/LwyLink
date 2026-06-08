#!/usr/bin/env python3
"""CLI example that calls the copy-paste friendly lwylink.py library."""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

from lwylink import DEFAULT_API, ConnectOptions, NetEngine, env_bool, install_sigint_stop, parse_event


REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_EXE = REPO_ROOT / "build" / "lwylink-net.exe"


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()

    if args.self_test:
        return self_test(args.exe)
    if args.command == "connect":
        return run_connect(args)
    if args.command == "cleanup":
        return run_cleanup(args)

    parser.print_help()
    return 2


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Python example for lwylink-net.exe")
    parser.add_argument("--exe", type=Path, default=DEFAULT_EXE, help="Path to lwylink-net.exe")
    parser.add_argument("--self-test", action="store_true", help="Verify that Python can launch lwylink-net.exe")

    subparsers = parser.add_subparsers(dest="command")

    connect = subparsers.add_parser("connect", help="Start the virtual network engine")
    connect.add_argument("--api", default=os.getenv("LWYLINK_API", DEFAULT_API))
    connect.add_argument("--token", default=os.getenv("LWYLINK_TOKEN", ""))
    connect.add_argument("--room-id", type=int, default=int(os.getenv("LWYLINK_ROOM_ID", "0") or "0"))
    connect.add_argument("--if-name", default=os.getenv("LWYLINK_IF", "LwyLink"))
    connect.add_argument("--p2p", action=argparse.BooleanOptionalAction, default=env_bool("LWYLINK_P2P", True))
    connect.add_argument("--user-id", type=int, default=int(os.getenv("LWYLINK_USER_ID", "0") or "0"))
    connect.add_argument("--room-name", default=os.getenv("LWYLINK_ROOM_NAME", ""))
    connect.add_argument("--room-public-key", default=os.getenv("LWYLINK_ROOM_PUBLIC_KEY", ""))
    connect.add_argument("--network-cidr", default=os.getenv("LWYLINK_NETWORK_CIDR", ""))
    connect.add_argument("--overlay-addr", default=os.getenv("LWYLINK_OVERLAY_ADDR", ""))
    connect.add_argument("--udp-overlay-addr", default=os.getenv("LWYLINK_UDP_OVERLAY_ADDR", ""))
    connect.add_argument("--server-public-key", default=os.getenv("LWYLINK_SERVER_PUBLIC_KEY", ""))

    cleanup = subparsers.add_parser("cleanup", help="Cleanup routes and firewall state")
    cleanup.add_argument("--if-name", default=os.getenv("LWYLINK_IF", "LwyLink"))
    cleanup.add_argument("--cidr", default=os.getenv("LWYLINK_NETWORK_CIDR", ""))

    return parser


def self_test(exe: Path) -> int:
    engine = NetEngine(exe)
    completed = subprocess.run(
        [str(engine.exe), "--help"],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    output = completed.stdout + completed.stderr
    if completed.returncode != 0:
        print(output, file=sys.stderr)
        return completed.returncode
    if "lwylink-net.exe connect" not in output:
        print("unexpected help output from lwylink-net.exe", file=sys.stderr)
        print(output, file=sys.stderr)
        return 1

    process = subprocess.Popen(
        [
            str(engine.exe),
            "connect",
            "--token",
            "self-test-token",
            "--user-id",
            "1",
            "--room-id",
            "1",
            "--room-public-key",
            "self-test-room-key",
            "--network-cidr",
            "172.16.1.0/24",
            "--overlay-addr",
            "127.0.0.1:1",
            "--server-public-key",
            "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            "--p2p=false",
        ],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        bufsize=1,
    )
    events: list[dict[str, Any]] = []
    try:
        assert process.stdout is not None
        for _ in range(4):
            line = process.stdout.readline()
            if not line:
                break
            events.append(parse_event(line.strip()))
            event_types = {event.get("type") for event in events}
            if "preparing" in event_types and "connecting" in event_types:
                break
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=3)

    event_types = {event.get("type") for event in events}
    if "preparing" not in event_types or "connecting" not in event_types:
        print("self-test did not receive expected JSON events", file=sys.stderr)
        print(events, file=sys.stderr)
        return 1

    print("self-test ok: Python launched lwylink-net.exe and parsed JSON status events")
    return 0


def run_connect(args: argparse.Namespace) -> int:
    engine = NetEngine(args.exe)
    install_sigint_stop(engine)
    engine.start(
        ConnectOptions(
            api=args.api,
            token=args.token,
            room_id=args.room_id,
            if_name=args.if_name,
            p2p=args.p2p,
            user_id=args.user_id,
            room_name=args.room_name,
            room_public_key=args.room_public_key,
            network_cidr=args.network_cidr,
            overlay_addr=args.overlay_addr,
            udp_overlay_addr=args.udp_overlay_addr,
            server_public_key=args.server_public_key,
        ),
        handle_event,
    )
    return engine.wait()


def run_cleanup(args: argparse.Namespace) -> int:
    return NetEngine(args.exe).cleanup(args.if_name, args.cidr)


def handle_event(event: dict[str, Any]) -> None:
    event_type = event.get("type", "unknown")
    if event_type == "connected":
        print(f"connected ip={event.get('ip', '-')} gateway={event.get('gateway', '-')} mtu={event.get('mtu', '-')}")
    elif event_type == "transport":
        print(f"transport mode={event.get('transportMode', '-')} message={event.get('transportMessage', '')}")
    elif event_type == "error":
        print(f"error: {event.get('message', '')}", file=sys.stderr)
    else:
        message = event.get("message", "")
        print(f"{event_type}: {message}" if message else event_type)


if __name__ == "__main__":
    raise SystemExit(main())
