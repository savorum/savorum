#!/usr/bin/env python3
"""Minimal Savorum receiver with signature verification and durable deduplication.

Set SAVORUM_WEBHOOK_SECRET to the once-shown subscription secret, then run:
    python3 webhook_receiver.py
Expose 127.0.0.1:9001 behind your HTTPS reverse proxy and use that HTTPS URL
in Savorum's Integrations settings. Savorum must be able to reach the URL;
private destinations require --webhook-trusted-networks on the Savorum server.
Send a test from settings. A successful delivery appears in receiver-events.db.
Do not commit secrets or this database. The example binds loopback only.
"""

import hashlib
import hmac
import json
import os
import sqlite3
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

MAX_BODY = 64 * 1024


def verify(secret, signature, body, now):
    """Verify raw bytes before JSON parsing; compare digests in constant time."""
    try:
        stamp_part, digest_part = signature.split(",")
        if not stamp_part.startswith("t=") or not digest_part.startswith("v1="):
            return False
        timestamp = int(stamp_part[2:])
        if abs(now - timestamp) > 300:
            return False
        supplied = bytes.fromhex(digest_part[3:])
        expected = hmac.digest(secret.encode(), stamp_part[2:].encode() + b"." + body, "sha256")
        return hmac.compare_digest(expected, supplied)
    except (ValueError, TypeError):
        return False


def serve(secret, database="receiver-events.db"):
    os.umask(0o077)
    connection = sqlite3.connect(database)
    connection.execute("CREATE TABLE IF NOT EXISTS deliveries (id TEXT PRIMARY KEY, event_id TEXT NOT NULL, event_type TEXT NOT NULL, received_at INTEGER NOT NULL, body_sha256 TEXT NOT NULL)")
    connection.commit()

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            self.connection.settimeout(10)
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if not 0 < length <= MAX_BODY:
                    self.send_error(413)
                    return
                body = self.rfile.read(length)
                if len(body) != length or not verify(secret, self.headers.get("Savorum-Signature", ""), body, time.time()):
                    self.send_error(401)
                    return
                payload = json.loads(body)
                delivery_id = payload["delivery_id"]
                event = payload["event"]
                if not isinstance(delivery_id, str) or not delivery_id or event["version"] != 1:
                    self.send_error(400)
                    return
                digest = hashlib.sha256(body).hexdigest()
                existing = connection.execute("SELECT body_sha256 FROM deliveries WHERE id=?", (delivery_id,)).fetchone()
                if existing and existing[0] != digest:
                    self.send_error(409)
                    return
                # Commit deduplication and your local work in the same transaction.
                # For nontransactional external work, use a durable local queue.
                with connection:
                    connection.execute("INSERT OR IGNORE INTO deliveries VALUES(?,?,?,?,?)", (delivery_id, event["id"], event["type"], int(time.time()), digest))
                self.send_response(204)
                self.end_headers()
            except (ValueError, KeyError, TypeError):
                self.send_error(400)

        def log_message(self, _format, *_args):
            pass  # Never log request headers, bodies, or signing secrets.

    try:
        HTTPServer(("127.0.0.1", 9001), Handler).serve_forever()
    finally:
        connection.close()


if __name__ == "__main__":
    configured_secret = os.environ.get("SAVORUM_WEBHOOK_SECRET", "")
    if not configured_secret:
        raise SystemExit("Set SAVORUM_WEBHOOK_SECRET before starting the receiver.")
    serve(configured_secret)
