#!/usr/bin/env python3
"""Example Python wrapper for the LwyLink virtual network engine.

The wrapper treats lwylink-net.exe as an external process and consumes its
stdout as JSON Lines. This keeps Python out of the Wintun, routing, encryption,
and UDP direct-path details.
"""

from __future__ import annotations

import argparse
import json
import os
import signal
import subprocess
import sys
from pathlib import Path
from typing import Any, Iterable


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
    connect.add_argument("--api", default=os.getenv("LWYLINK_API", "http://127.0.0.1:8080"))
    connect.add_argument("--token", default=os.getenv("LWYLINK_TOKEN", ""))
    connect.add_argument("--room-id", type=int, default=int(os.getenv("LWYLINK_ROOM_ID", "0") or "0"))
    connect.add_argument("--if-name", default=os.getenv("LWYLINK_IF", "LwyLink"))
    connect.add_argument("--p2p", action=argparse.BooleanOptionalAction, default=parse_bool(os.getenv("LWYLINK_P2P", "true")))
    connect.add_argument("--user-id", type=int, default=int(os.getenv("LWYLINK_USER_ID", "0") or "0"))
    connect.add_argument("--room-name", default=os.getenv("LWYLINK_ROOM_NAME", ""))
    connect.add_argument("--room-public-key", default=os.getenv("LWYLINK_ROOM_PUBLIC_KEY", ""))
    connect.add_argument("--network-cidr", default=os.getenv("LWYLINK_NETWORK_CIDR", ""))
    connect.add_argument("--overlay-addr", default=os.getenv("LWYLINK_OVERLAY_ADDR", ""))
    connect.add_argument("--server-public-key", default=os.getenv("LWYLINK_SERVER_PUBLIC_KEY", ""))

    cleanup = subparsers.add_parser("cleanup", help="Cleanup routes and firewall state")
    cleanup.add_argument("--if-name", default=os.getenv("LWYLINK_IF", "LwyLink"))
    cleanup.add_argument("--cidr", default=os.getenv("LWYLINK_NETWORK_CIDR", ""))

    return parser


def self_test(exe: Path) -> int:
    exe = resolve_exe(exe)
    completed = subprocess.run(
        [str(exe), "--help"],
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

    probe = subprocess.Popen(
        [
            str(exe),
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
        stderr=subprocess.PIPE,
        bufsize=1,
    )
    events: list[dict[str, Any]] = []
    try:
        assert probe.stdout is not None
        for _ in range(4):
            line = probe.stdout.readline()
            if not line:
                break
            events.append(parse_event(line.strip()))
            event_types = {event.get("type") for event in events}
            if "preparing" in event_types and "connecting" in event_types:
                break
    finally:
        if probe.poll() is None:
            probe.terminate()
            try:
                probe.wait(timeout=3)
            except subprocess.TimeoutExpired:
                probe.kill()
                probe.wait(timeout=3)

    event_types = {event.get("type") for event in events}
    if "preparing" not in event_types or "connecting" not in event_types:
        print("self-test did not receive expected JSON events", file=sys.stderr)
        print(json.dumps(events, ensure_ascii=False), file=sys.stderr)
        if probe.stderr is not None:
            print(probe.stderr.read(), file=sys.stderr)
        return 1

    print("self-test ok: Python launched lwylink-net.exe and parsed JSON status events")
    return 0


def run_connect(args: argparse.Namespace) -> int:
    exe = resolve_exe(args.exe)
    command = [
        str(exe),
        "connect",
        "--api",
        args.api,
        "--if",
        args.if_name,
        "--p2p=" + bool_text(args.p2p),
    ]
    if args.token:
        command += ["--token", args.token]
    if args.room_id > 0:
        command += ["--room-id", str(args.room_id)]
    command += direct_connect_args(args)

    return stream_process(command)


def direct_connect_args(args: argparse.Namespace) -> list[str]:
    fields = [
        ("--user-id", args.user_id),
        ("--room-name", args.room_name),
        ("--room-public-key", args.room_public_key),
        ("--network-cidr", args.network_cidr),
        ("--overlay-addr", args.overlay_addr),
        ("--server-public-key", args.server_public_key),
    ]
    out: list[str] = []
    for flag, value in fields:
        if value:
            out += [flag, str(value)]
    return out


def run_cleanup(args: argparse.Namespace) -> int:
    exe = resolve_exe(args.exe)
    command = [str(exe), "cleanup", "--if", args.if_name]
    if args.cidr:
        command += ["--cidr", args.cidr]
    return stream_process(command)


def stream_process(command: Iterable[str]) -> int:
    process = subprocess.Popen(
        list(command),
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        bufsize=1,
    )

    def stop_child(signum: int, _frame: Any) -> None:
        del signum
        if process.poll() is None:
            process.terminate()

    previous_sigint = signal.signal(signal.SIGINT, stop_child)
    try:
        assert process.stdout is not None
        for line in process.stdout:
            line = line.strip()
            if not line:
                continue
            handle_event(parse_event(line))
        stderr = process.stderr.read() if process.stderr else ""
        if stderr.strip():
            print(stderr.strip(), file=sys.stderr)
        return process.wait()
    finally:
        signal.signal(signal.SIGINT, previous_sigint)
        if process.poll() is None:
            process.terminate()


def parse_event(line: str) -> dict[str, Any]:
    try:
        value = json.loads(line)
    except json.JSONDecodeError:
        return {"type": "raw", "message": line}
    if isinstance(value, dict):
        return value
    return {"type": "raw", "message": line}


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


def resolve_exe(path: Path) -> Path:
    resolved = path.expanduser()
    if not resolved.is_absolute():
        resolved = (Path.cwd() / resolved).resolve()
    if not resolved.exists():
        raise SystemExit(f"lwylink-net.exe not found: {resolved}")
    return resolved


def parse_bool(value: str) -> bool:
    return value.strip().lower() not in {"0", "false", "no", "off"}


def bool_text(value: bool) -> str:
    return "true" if value else "false"


if __name__ == "__main__":
    raise SystemExit(main())
