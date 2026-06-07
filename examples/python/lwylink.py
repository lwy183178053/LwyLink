#!/usr/bin/env python3
"""Small copy-paste friendly Python library for LwyLink.

It covers two integration layers:

- RoomClient: HTTP JSON API and SSE room events.
- NetEngine: starts/stops lwylink-net.exe and parses JSON Lines status events.

Only the Python standard library is used.
"""

from __future__ import annotations

import json
import os
import signal
import subprocess
import threading
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Iterator


DEFAULT_API = "http://127.0.0.1:8080"


class LwyLinkError(RuntimeError):
    pass


@dataclass
class RoomClient:
    api: str = DEFAULT_API
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
            raise LwyLinkError(f"http {exc.code}: {message}") from exc
        return json.loads(raw) if raw.strip() else None

    def register(self, email: str, password: str, display_name: str = "") -> dict[str, Any]:
        result = self.request("POST", "/api/register", {"email": email, "password": password, "displayName": display_name})
        self.token = result["token"]
        return result

    def login(self, email: str, password: str) -> dict[str, Any]:
        result = self.request("POST", "/api/login", {"email": email, "password": password})
        self.token = result["token"]
        return result

    def me(self) -> dict[str, Any]:
        return self.request("GET", "/api/me")

    def announcement(self) -> str:
        return self.request("GET", "/api/announcement").get("announcement", "")

    def rooms(self) -> list[dict[str, Any]]:
        return self.request("GET", "/api/rooms")

    def create_room(self, name: str, max_members: int = 8) -> dict[str, Any]:
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

    def room_events(self) -> Iterator[dict[str, Any]]:
        headers = {}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        req = urllib.request.Request(self.api.rstrip("/") + "/api/rooms/events", method="GET", headers=headers)
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


@dataclass
class ConnectOptions:
    token: str
    api: str = DEFAULT_API
    room_id: int = 0
    if_name: str = "LwyLink"
    p2p: bool = True
    user_id: int = 0
    room_name: str = ""
    room_public_key: str = ""
    network_cidr: str = ""
    overlay_addr: str = ""
    udp_overlay_addr: str = ""
    server_public_key: str = ""


class NetEngine:
    def __init__(self, exe: str | Path):
        self.exe = resolve_exe(Path(exe))
        self.process: subprocess.Popen[str] | None = None
        self._thread: threading.Thread | None = None

    def start(self, options: ConnectOptions, on_event: Callable[[dict[str, Any]], None] | None = None) -> None:
        if self.process and self.process.poll() is None:
            raise LwyLinkError("lwylink-net.exe is already running")
        command = [
            str(self.exe),
            "connect",
            "--api",
            options.api,
            "--if",
            options.if_name,
            "--p2p=" + bool_text(options.p2p),
        ]
        if options.token:
            command += ["--token", options.token]
        if options.room_id > 0:
            command += ["--room-id", str(options.room_id)]
        command += direct_connect_args(options)
        self.process = subprocess.Popen(command, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, bufsize=1)
        self._thread = threading.Thread(target=self._read_events, args=(on_event,), daemon=True)
        self._thread.start()

    def wait(self) -> int:
        if not self.process:
            return 0
        return self.process.wait()

    def stop(self, timeout: float = 5) -> None:
        process = self.process
        if not process or process.poll() is not None:
            return
        process.terminate()
        try:
            process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=timeout)

    def cleanup(self, if_name: str = "LwyLink", cidr: str = "") -> int:
        command = [str(self.exe), "cleanup", "--if", if_name]
        if cidr:
            command += ["--cidr", cidr]
        completed = subprocess.run(command, text=True, check=False)
        return completed.returncode

    def _read_events(self, on_event: Callable[[dict[str, Any]], None] | None) -> None:
        assert self.process is not None and self.process.stdout is not None
        for line in self.process.stdout:
            event = parse_event(line.strip())
            if on_event:
                on_event(event)


def direct_connect_args(options: ConnectOptions) -> list[str]:
    fields = [
        ("--user-id", options.user_id),
        ("--room-name", options.room_name),
        ("--room-public-key", options.room_public_key),
        ("--network-cidr", options.network_cidr),
        ("--overlay-addr", options.overlay_addr),
        ("--udp-overlay-addr", options.udp_overlay_addr),
        ("--server-public-key", options.server_public_key),
    ]
    out: list[str] = []
    for flag, value in fields:
        if value:
            out += [flag, str(value)]
    return out


def parse_event(line: str) -> dict[str, Any]:
    try:
        value = json.loads(line)
    except json.JSONDecodeError:
        return {"type": "raw", "message": line}
    return value if isinstance(value, dict) else {"type": "raw", "message": line}


def resolve_exe(path: Path) -> Path:
    resolved = path.expanduser()
    if not resolved.is_absolute():
        resolved = (Path.cwd() / resolved).resolve()
    if not resolved.exists():
        raise LwyLinkError(f"lwylink-net.exe not found: {resolved}")
    return resolved


def env_bool(key: str, fallback: bool) -> bool:
    value = os.getenv(key, "")
    if not value:
        return fallback
    return value.strip().lower() not in {"0", "false", "no", "off"}


def bool_text(value: bool) -> str:
    return "true" if value else "false"


def install_sigint_stop(engine: NetEngine) -> None:
    previous = signal.getsignal(signal.SIGINT)

    def stop_child(signum: int, frame: Any) -> None:
        engine.stop()
        if callable(previous):
            previous(signum, frame)

    signal.signal(signal.SIGINT, stop_child)
