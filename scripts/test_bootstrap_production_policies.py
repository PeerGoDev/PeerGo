from __future__ import annotations

import importlib.util
import sys
import tempfile
import threading
import unittest
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


SCRIPT = Path(__file__).with_name("bootstrap_production_policies.py")
SPEC = importlib.util.spec_from_file_location("bootstrap_production_policies", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class ProductionPolicyBootstrapTest(unittest.TestCase):
    def test_resolve_runtime_accepts_only_single_server_loopback(self) -> None:
        endpoint, origin = MODULE.resolve_runtime(
            {
                "PEERGO_DEPLOYMENT_MODE": "single-server",
                "PEERGO_WEB_BIND_ADDRESS": "127.0.0.1",
                "PEERGO_WEB_HOST_PORT": "18080",
                "PEERGO_PUBLIC_ORIGIN": "https://rousi.pro",
            }
        )
        self.assertEqual(endpoint, "http://127.0.0.1:18080")
        self.assertEqual(origin, "https://rousi.pro")

        with self.assertRaises(MODULE.BootstrapError):
            MODULE.resolve_runtime(
                {
                    "PEERGO_DEPLOYMENT_MODE": "single-server",
                    "PEERGO_WEB_BIND_ADDRESS": "0.0.0.0",
                    "PEERGO_PUBLIC_ORIGIN": "https://rousi.pro",
                }
            )

    def test_load_env_file_does_not_expand_values(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / ".env.production"
            path.write_text(
                "PEERGO_PUBLIC_ORIGIN='https://rousi.pro'\nSECRET=$DO_NOT_EXPAND\n",
                encoding="utf-8",
            )
            values = MODULE.load_env_file(path)
        self.assertEqual(values["PEERGO_PUBLIC_ORIGIN"], "https://rousi.pro")
        self.assertEqual(values["SECRET"], "$DO_NOT_EXPAND")

    def test_registration_update_preserves_every_setting_except_mode(self) -> None:
        current = {name: f"value-{name}" for name in MODULE.REGISTRATION_FIELDS}
        current.update(
            {
                "member_invites_enabled": False,
                "version": 7,
                "mode": "closed",
                "human_verification_secret_configured": True,
            }
        )
        payload = MODULE.registration_update_payload(current)
        self.assertEqual(payload["mode"], "invite")
        self.assertEqual(payload["expected_version"], 7)
        self.assertFalse(payload["member_invites_enabled"])
        self.assertNotIn("human_verification_secret_configured", payload)

    def test_effective_time_has_margin_and_honors_server_minimum(self) -> None:
        now = datetime(2026, 8, 20, 16, 45, tzinfo=timezone.utc)
        server_minimum = now + timedelta(minutes=9)
        effective = MODULE.choose_effective_at(now, MODULE.format_rfc3339(server_minimum))
        self.assertEqual(effective, server_minimum + timedelta(seconds=5))

    def test_existing_scheduled_revisions_prevent_duplicate_issue(self) -> None:
        newcomer = {
            "current": {"enabled": False},
            "items": [{"enabled": True, "timeline_state": "scheduled"}],
        }
        hnr = {"current": {"configured": False}, "items": [{"delivery_state": "pending"}]}
        self.assertTrue(MODULE.newcomer_has_enabled_revision(newcomer))
        self.assertTrue(MODULE.hnr_has_revision(hnr))
        self.assertFalse(MODULE.newcomer_ready(newcomer))
        self.assertFalse(MODULE.hnr_ready(hnr))

        historical = {
            "current": {"enabled": False},
            "items": [{"enabled": True, "timeline_state": "active"}],
        }
        self.assertFalse(MODULE.newcomer_has_enabled_revision(historical))

    def test_api_client_keeps_credentials_in_memory_and_sends_write_boundaries(self) -> None:
        observed = {}

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, _format, *_args):
                return

            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                observed["login_body"] = self.rfile.read(length).decode("utf-8")
                observed["login_origin"] = self.headers.get("Origin")
                observed["login_host"] = self.headers.get("Host")
                observed["forwarded_proto"] = self.headers.get("X-Forwarded-Proto")
                body = b'{"csrf_token":"csrf-value","user":{"username":"admin"}}'
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Set-Cookie", "__Host-peergo_session=session-value; Path=/; Secure; HttpOnly")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def do_PUT(self):
                length = int(self.headers.get("Content-Length", "0"))
                observed["write_body"] = self.rfile.read(length).decode("utf-8")
                observed["write_origin"] = self.headers.get("Origin")
                observed["write_cookie"] = self.headers.get("Cookie")
                observed["write_csrf"] = self.headers.get("X-CSRF-Token")
                body = b'{"mode":"invite"}'
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            client = MODULE.ApiClient(
                endpoint=f"http://127.0.0.1:{server.server_port}",
                public_origin="https://rousi.pro",
            )
            client.login("admin", "secret-password", lambda: "")
            client.call("PUT", "/api/v1/admin/settings/registration", {"mode": "invite"}, csrf=True)
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)

        self.assertIn('"password":"secret-password"', observed["login_body"])
        self.assertEqual(observed["login_origin"], "https://rousi.pro")
        self.assertEqual(observed["login_host"], "rousi.pro")
        self.assertEqual(observed["forwarded_proto"], "https")
        self.assertEqual(observed["write_origin"], "https://rousi.pro")
        self.assertEqual(observed["write_cookie"], "__Host-peergo_session=session-value")
        self.assertEqual(observed["write_csrf"], "csrf-value")


if __name__ == "__main__":
    unittest.main()
