"""Minimal black-box SSE client for the conformance suite (stdlib only).

Opens ``GET /api/events`` against ``OC_TARGET_URL`` and parses the raw SSE wire
into event dicts on a background thread, pushing them onto a queue the test
consumes with SHORT bounded waits. Design goals:

  * pure HTTP — no server-implementation imports (black-box iron rule);
  * bounded time — every wait is an explicit ``timeout`` (default 5 s); the
    suite never blocks on the 15 s heartbeat cadence because tests always
    TRIGGER the event they wait for (a write fans the delta within ms);
  * raw fidelity — the parser keeps the ``id:`` line / comment / ``data:``
    fields separate so tests can assert frame SHAPE (e.g. "directed band frames
    carry no id line"), not just payload content.

Parsed event dict:  {"comment": str|None, "id": str|None, "data": str|None}
(one SSE event = the lines up to a blank line; multiple data lines are joined
with "\n" per the SSE spec, though the server never emits multi-line data).
"""

from __future__ import annotations

import http.client
import json
import queue
import socket
import threading
from typing import Any
from urllib.parse import urlsplit

CONNECT_TIMEOUT = 5.0
# Longer than the 15 s heartbeat so a healthy stream never times out mid-read.
READ_TIMEOUT = 20.0


class SSEConnection:
    """One live /api/events connection; events arrive on ``self.events``."""

    def __init__(self, base_url: str, token: str) -> None:
        self.events: "queue.Queue[dict[str, Any]]" = queue.Queue()
        self.status_code: int | None = None
        self.headers: http.client.HTTPMessage | None = None
        self.error_body: bytes = b""
        self._closed = threading.Event()
        parsed = urlsplit(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.hostname:
            raise ValueError(f"SSEConnection needs an HTTP(S) base URL, got {base_url!r}")
        connection_type = (
            http.client.HTTPSConnection if parsed.scheme == "https" else http.client.HTTPConnection
        )
        self._client = connection_type(
            parsed.hostname, parsed.port, timeout=CONNECT_TIMEOUT
        )
        self._client.request("GET", "/api/events", headers={"Authorization": f"Bearer {token}"})
        # HTTPConnection uses its constructor timeout for both connect and
        # header reads. Preserve the old 5 s connect / 20 s read contract
        # before getresponse waits for the SSE response headers.
        if self._client.sock is not None:
            self._client.sock.settimeout(READ_TIMEOUT)
        self._resp = self._client.getresponse()
        self.status_code = self._resp.status
        self.headers = self._resp.headers
        if self._resp.status != 200:
            # Refused before any stream bytes (401 / 409): capture the JSON body
            # and DON'T start the reader thread.
            self.error_body = self._resp.read()
            self._client.close()
            self._closed.set()
            return
        self._thread = threading.Thread(target=self._pump, daemon=True)
        self._thread.start()

    # ── reader ────────────────────────────────────────────────────────────────
    def _pump(self) -> None:
        buf = b""
        try:
            while True:
                # read1 returns as soon as bytes are buffered; read(4096) is
                # allowed to wait for a full buffer and would hide small SSE
                # frames until the heartbeat fills it.
                chunk = self._resp.read1(4096)
                if not chunk:
                    break
                buf += chunk
                while b"\n\n" in buf:
                    raw, buf = buf.split(b"\n\n", 1)
                    self._emit(raw.decode("utf-8", errors="replace"))
        except Exception:
            pass  # closed / timed out — the queue simply stops growing
        finally:
            self._closed.set()

    def _emit(self, raw: str) -> None:
        event: dict[str, Any] = {"comment": None, "id": None, "data": None}
        data_lines: list[str] = []
        for line in raw.split("\n"):
            if line.startswith(":"):
                event["comment"] = line[1:].strip()
            elif line.startswith("id:"):
                event["id"] = line[3:].strip()
            elif line.startswith("data:"):
                data_lines.append(line[5:].lstrip())
        if data_lines:
            event["data"] = "\n".join(data_lines)
        self.events.put(event)

    # ── consumers ─────────────────────────────────────────────────────────────
    def next_event(self, timeout: float = 5.0) -> dict[str, Any]:
        """Next raw SSE event (comment or data), or raise on timeout."""
        return self.events.get(timeout=timeout)

    def wait_for(self, predicate, timeout: float = 5.0) -> dict[str, Any]:
        """Drain events until one satisfies ``predicate``; raise on timeout."""
        import time as _time

        deadline = _time.monotonic() + timeout
        while True:
            remaining = deadline - _time.monotonic()
            if remaining <= 0:
                raise TimeoutError(
                    "no matching SSE event within "
                    f"{timeout}s (predicate={predicate!r})"
                )
            try:
                event = self.events.get(timeout=remaining)
            except queue.Empty:
                raise TimeoutError(
                    f"no matching SSE event within {timeout}s "
                    f"(predicate={predicate!r})"
                ) from None
            if predicate(event):
                return event

    def wait_for_frame(self, topic: str, timeout: float = 5.0) -> dict[str, Any]:
        """Next DELTA frame (data event) whose JSON ``topic`` matches; returns
        {"event": <raw>, "frame": <parsed data json>}."""

        def _match(ev: dict[str, Any]) -> bool:
            if ev.get("data") is None:
                return False
            try:
                return json.loads(ev["data"]).get("topic") == topic
            except (ValueError, AttributeError):
                return False

        ev = self.wait_for(_match, timeout=timeout)
        return {"event": ev, "frame": json.loads(ev["data"])}

    def wait_closed(self, timeout: float = 5.0) -> bool:
        """True once the stream ENDED server-side (the pump thread finished —
        EOF / terminal chunk / read error). The §5.1 takeover observation
        point: a displaced listener's stream is terminated by the server."""
        return self._closed.wait(timeout=timeout)

    def assert_quiet(self, timeout: float = 1.0, ignore_comments: bool = True) -> None:
        """Assert NO (non-comment) event arrives within ``timeout`` seconds —
        the bounded negative wait for MUST-NOT-emit assertions."""
        import time as _time

        deadline = _time.monotonic() + timeout
        while True:
            remaining = deadline - _time.monotonic()
            if remaining <= 0:
                return
            try:
                ev = self.events.get(timeout=remaining)
            except queue.Empty:
                return
            if ignore_comments and ev.get("data") is None:
                continue
            raise AssertionError(f"expected quiet stream, got event: {ev}")

    # ── lifecycle ─────────────────────────────────────────────────────────────
    def close(self, wait: float = 0.0) -> None:
        """Close the connection (idempotent). The TCP close is what the SERVER
        observes (its disconnect edge fires on the socket drop); the local pump
        thread is a daemon that dies on its own — tests that depend on the
        server-side edge POLL the observable surface instead of waiting here."""
        # A cross-thread close of an HTTP/1.1 response need not interrupt a
        # blocked read on Linux. Shut down the socket first so this client's
        # pump wakes immediately and the server observes the TCP disconnect.
        sock = self._client.sock
        if sock is not None:
            try:
                sock.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
        try:
            self._resp.close()
        except Exception:
            pass
        self._client.close()
        if wait > 0:
            self._closed.wait(timeout=wait)

    def __enter__(self) -> "SSEConnection":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()
