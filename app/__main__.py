import hmac
import json
import os
import sqlite3
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit


DB_PATH = Path(os.environ.get("DB_PATH", "data/miner.sqlite3"))
API_TOKEN = os.environ.get("API_TOKEN", "")
MAX_BODY = 2 * 1024 * 1024


def connect():
    return sqlite3.connect(DB_PATH, timeout=30)


def initialize():
    DB_PATH.parent.mkdir(parents=True, exist_ok=True)
    with connect() as db:
        db.execute("PRAGMA journal_mode=WAL")
        db.execute("""CREATE TABLE IF NOT EXISTS listings (
            source TEXT NOT NULL,
            external_id TEXT NOT NULL,
            payload TEXT NOT NULL,
            first_seen TEXT NOT NULL,
            updated_at TEXT NOT NULL,
            PRIMARY KEY (source, external_id)
        )""")


class Handler(BaseHTTPRequestHandler):
    def reply(self, status, payload):
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def authorized(self):
        expected = f"Bearer {API_TOKEN}".encode()
        actual = self.headers.get("Authorization", "").encode()
        if not hmac.compare_digest(actual, expected):
            self.reply(401, {"error": "Unauthorized"})
            return False
        return True

    def do_GET(self):
        parsed = urlsplit(self.path)
        if parsed.path == "/health":
            self.reply(200, {"status": "ok"})
            return
        if not self.authorized():
            return
        if parsed.path != "/listings":
            self.reply(404, {"error": "Not found"})
            return
        try:
            limit = int(parse_qs(parsed.query).get("limit", ["100"])[0])
            if not 1 <= limit <= 1000:
                raise ValueError()
        except ValueError:
            self.reply(400, {"error": "limit must be between 1 and 1000"})
            return
        with connect() as db:
            rows = db.execute(
                "SELECT payload, first_seen, updated_at FROM listings ORDER BY updated_at DESC LIMIT ?",
                (limit,),
            ).fetchall()
        self.reply(200, {"items": [
            {"data": json.loads(payload), "first_seen": first, "updated_at": updated}
            for payload, first, updated in rows
        ]})

    def do_POST(self):
        if not self.authorized():
            return
        if urlsplit(self.path).path != "/listings":
            self.reply(404, {"error": "Not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= MAX_BODY:
                self.reply(413, {"error": "Body must be between 1 byte and 2 MiB"})
                return
            records = json.loads(self.rfile.read(length))
            if not isinstance(records, list) or not 1 <= len(records) <= 1000:
                raise ValueError("Expected an array of 1 to 1000 listings")
            for record in records:
                if not isinstance(record, dict) or any(
                    not isinstance(record.get(key), str) or not record[key].strip()
                    for key in ("source", "external_id", "title", "url")
                ):
                    raise ValueError("Every listing requires nonempty source, external_id, title and url strings")
                if urlsplit(record["url"]).scheme not in ("http", "https"):
                    raise ValueError("Listing url must use HTTP or HTTPS")
        except (ValueError, UnicodeDecodeError) as error:
            self.reply(400, {"error": str(error)})
            return
        now = datetime.now(timezone.utc).isoformat()
        with connect() as db:
            db.executemany("""INSERT INTO listings VALUES (?, ?, ?, ?, ?)
                ON CONFLICT(source, external_id) DO UPDATE SET
                payload=excluded.payload, updated_at=excluded.updated_at""", [
                (r["source"], r["external_id"], json.dumps(r, ensure_ascii=False), now, now)
                for r in records
            ])
        self.reply(200, {"processed": len(records)})


def main():
    if len(API_TOKEN) < 24:
        raise SystemExit("Set API_TOKEN to a random secret of at least 24 characters")
    initialize()
    address = (os.environ.get("HOST", "127.0.0.1"), int(os.environ.get("PORT", "8000")))
    server = ThreadingHTTPServer(address, Handler)
    server.daemon_threads = True
    print(f"realty-data-miner listening on {address[0]}:{address[1]}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
