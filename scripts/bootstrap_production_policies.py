#!/usr/bin/env python3
"""Issue the minimum audited production policies through PeerGo's admin API.

This command is intentionally limited to a loopback-bound production Web
listener. It never reads a password from argv, an environment variable or a
file, and it does not write policy tables directly.
"""

from __future__ import annotations

import argparse
import getpass
import ipaddress
import json
import os
import sys
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from http.cookies import SimpleCookie
from pathlib import Path
from typing import Any, Callable
from urllib import error, parse, request


CONFIRMATION = "APPLY_PEERGO_PRODUCTION_POLICIES"
SESSION_COOKIE_NAMES = ("__Host-peergo_session", "peergo_session")
REGISTRATION_FIELDS = (
    "member_invites_enabled",
    "invite_valid_days",
    "max_invites_per_member",
    "minimum_invite_account_age_days",
    "minimum_invite_level",
    "username_min_characters",
    "username_max_characters",
    "reserved_usernames",
    "email_domain_mode",
    "email_domains",
    "session_valid_hours",
    "remember_session_valid_hours",
    "human_verification_provider",
    "human_verification_site_key",
    "human_verification_registration_enabled",
    "human_verification_login_enabled",
    "human_verification_password_recovery_enabled",
)
DEFAULT_NEWCOMER_POLICY = {
    "enabled": True,
    "duration_seconds": 30 * 24 * 60 * 60,
    "minimum_credited_upload_bytes": str(50 * 1024 * 1024 * 1024),
    "minimum_seeding_active_seconds": 72 * 60 * 60,
}
DEFAULT_HNR_POLICY = {
    "mode": "enforced",
    "required_seed_seconds": 72 * 60 * 60,
    "required_ratio_basis_points": 10_000,
    "assessment_window_seconds": 7 * 24 * 60 * 60,
    "grace_period_seconds": 24 * 60 * 60,
    "max_interval_credit_seconds": 60 * 60,
}


class BootstrapError(RuntimeError):
    """A safe, operator-facing bootstrap failure."""


class ApiError(BootstrapError):
    def __init__(self, status: int, payload: dict[str, Any] | None) -> None:
        self.status = status
        self.payload = payload or {}
        code = self.payload.get("code", "http_error")
        title = self.payload.get("title", "请求失败")
        detail = self.payload.get("detail", "服务未返回可读错误详情。")
        request_id = self.payload.get("request_id", "")
        suffix = f" request_id={request_id}" if request_id else ""
        super().__init__(f"HTTP {status} {code}: {title}；{detail}{suffix}")


class NoRedirect(request.HTTPRedirectHandler):
    def redirect_request(self, req: request.Request, fp: Any, code: int, msg: str, headers: Any, newurl: str) -> None:
        raise BootstrapError(f"管理 API 返回了不允许的重定向：HTTP {code}")


def load_env_file(path: Path) -> dict[str, str]:
    if not path.is_file() or path.is_symlink():
        raise BootstrapError(f"生产环境文件不可用或是符号链接：{path}")
    values: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        name, value = line.split("=", 1)
        name, value = name.strip(), value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
            value = value[1:-1]
        values[name] = value
    return values


def resolve_runtime(env: dict[str, str]) -> tuple[str, str]:
    if env.get("PEERGO_DEPLOYMENT_MODE") != "single-server":
        raise BootstrapError("该命令只允许用于 PEERGO_DEPLOYMENT_MODE=single-server。")

    bind_address = env.get("PEERGO_WEB_BIND_ADDRESS", "127.0.0.1").strip()
    if bind_address == "localhost":
        endpoint_host = "127.0.0.1"
    else:
        try:
            address = ipaddress.ip_address(bind_address)
        except ValueError as exc:
            raise BootstrapError("PEERGO_WEB_BIND_ADDRESS 必须是 loopback 地址。") from exc
        if not address.is_loopback:
            raise BootstrapError("拒绝通过非 loopback Web 监听器执行生产策略初始化。")
        endpoint_host = f"[{address}]" if address.version == 6 else str(address)

    raw_port = env.get("PEERGO_WEB_HOST_PORT", "8080").strip()
    try:
        port = int(raw_port)
    except ValueError as exc:
        raise BootstrapError("PEERGO_WEB_HOST_PORT 不是有效端口。") from exc
    if port < 1 or port > 65535:
        raise BootstrapError("PEERGO_WEB_HOST_PORT 超出有效范围。")

    public_origin = env.get("PEERGO_PUBLIC_ORIGIN", "").strip()
    parsed_origin = parse.urlsplit(public_origin)
    if (
        parsed_origin.scheme != "https"
        or not parsed_origin.hostname
        or parsed_origin.username is not None
        or parsed_origin.password is not None
        or parsed_origin.path not in ("", "/")
        or parsed_origin.query
        or parsed_origin.fragment
    ):
        raise BootstrapError("PEERGO_PUBLIC_ORIGIN 必须是无路径的 HTTPS origin。")
    return f"http://{endpoint_host}:{port}", public_origin.rstrip("/")


def parse_rfc3339(value: str) -> datetime:
    normalized = value[:-1] + "+00:00" if value.endswith("Z") else value
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError as exc:
        raise BootstrapError(f"服务返回了无效时间：{value}") from exc
    if parsed.tzinfo is None:
        raise BootstrapError(f"服务返回了无时区时间：{value}")
    return parsed.astimezone(timezone.utc)


def format_rfc3339(value: datetime) -> str:
    return value.astimezone(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def choose_effective_at(now: datetime, *minimum_values: str) -> datetime:
    # The services reject an issue time less than five minutes ahead. Keep a
    # full minute of transport/clock margin and also honor timeline minima.
    candidate = now.astimezone(timezone.utc) + timedelta(minutes=6)
    for raw_value in minimum_values:
        candidate = max(candidate, parse_rfc3339(raw_value) + timedelta(seconds=5))
    return candidate.replace(microsecond=0)


def registration_update_payload(current: dict[str, Any]) -> dict[str, Any]:
    missing = [name for name in (*REGISTRATION_FIELDS, "version") if name not in current]
    if missing:
        raise BootstrapError("注册策略响应缺少字段：" + ", ".join(missing))
    payload = {name: current[name] for name in REGISTRATION_FIELDS}
    payload.update(
        {
            "mode": "invite",
            "expected_version": current["version"],
            "reason": "生产上线前启用邀请注册准入策略。",
        }
    )
    return payload


def newcomer_ready(page: dict[str, Any]) -> bool:
    current = page.get("current")
    return isinstance(current, dict) and current.get("enabled") is True


def newcomer_has_enabled_revision(page: dict[str, Any]) -> bool:
    return newcomer_ready(page) or any(
        isinstance(item, dict)
        and item.get("enabled") is True
        and item.get("timeline_state") == "scheduled"
        for item in page.get("items", [])
    )


def hnr_ready(page: dict[str, Any]) -> bool:
    current = page.get("current")
    return isinstance(current, dict) and current.get("configured") is True


def hnr_has_revision(page: dict[str, Any]) -> bool:
    return hnr_ready(page) or any(isinstance(item, dict) for item in page.get("items", []))


@dataclass
class ApiClient:
    endpoint: str
    public_origin: str
    timeout_seconds: float = 20.0
    session_cookie: str = ""
    csrf_token: str = ""

    def __post_init__(self) -> None:
        # Ignore host proxy variables so the administrator password can only
        # travel over the explicitly validated loopback socket.
        self._opener = request.build_opener(request.ProxyHandler({}), NoRedirect())
        self._public_host = parse.urlsplit(self.public_origin).netloc

    def call(
        self,
        method: str,
        path: str,
        body: dict[str, Any] | None = None,
        *,
        csrf: bool = False,
        idempotency_key: str = "",
    ) -> tuple[dict[str, Any], Any]:
        if not path.startswith("/"):
            raise BootstrapError("管理 API 路径必须是绝对路径。")
        headers = {
            "Accept": "application/json",
            "Host": self._public_host,
            "X-Forwarded-Proto": "https",
        }
        if method not in ("GET", "HEAD", "OPTIONS"):
            headers["Origin"] = self.public_origin
        if self.session_cookie:
            headers["Cookie"] = self.session_cookie
        if csrf:
            if not self.csrf_token:
                raise BootstrapError("当前会话没有 CSRF token。")
            headers["X-CSRF-Token"] = self.csrf_token
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key
        encoded_body = None
        if body is not None:
            headers["Content-Type"] = "application/json"
            encoded_body = json.dumps(body, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        http_request = request.Request(
            self.endpoint + path,
            data=encoded_body,
            headers=headers,
            method=method,
        )
        try:
            with self._opener.open(http_request, timeout=self.timeout_seconds) as response:
                raw_body = response.read()
                response_headers = response.headers
        except error.HTTPError as exc:
            raw_body = exc.read()
            try:
                problem = json.loads(raw_body.decode("utf-8")) if raw_body else None
            except (UnicodeDecodeError, json.JSONDecodeError):
                problem = None
            raise ApiError(exc.code, problem if isinstance(problem, dict) else None) from None
        except (error.URLError, TimeoutError, OSError) as exc:
            raise BootstrapError(f"无法连接 loopback 管理 API：{exc}") from exc

        if not raw_body:
            payload: dict[str, Any] = {}
        else:
            try:
                decoded = json.loads(raw_body.decode("utf-8"))
            except (UnicodeDecodeError, json.JSONDecodeError) as exc:
                raise BootstrapError("管理 API 返回了非 JSON 响应。") from exc
            if not isinstance(decoded, dict):
                raise BootstrapError("管理 API 返回了非对象 JSON 响应。")
            payload = decoded
        return payload, response_headers

    def login(self, username: str, password: str, second_factor_provider: Callable[[], str]) -> None:
        login_body: dict[str, Any] = {
            "identifier": username,
            "password": password,
            "remember_me": False,
        }
        try:
            payload, headers = self.call("POST", "/api/v1/session", login_body)
        except ApiError as exc:
            if exc.status != 428 or exc.payload.get("code") != "second_factor_required":
                raise
            second_factor_code = second_factor_provider().strip()
            if not second_factor_code:
                raise BootstrapError("两步验证码不能为空。")
            login_body["second_factor_code"] = second_factor_code
            payload, headers = self.call("POST", "/api/v1/session", login_body)

        csrf_token = payload.get("csrf_token")
        if not isinstance(csrf_token, str) or not csrf_token:
            raise BootstrapError("登录响应缺少 CSRF token。")
        session_cookie = ""
        for value in headers.get_all("Set-Cookie", []):
            parsed_cookie = SimpleCookie()
            parsed_cookie.load(value)
            for name in SESSION_COOKIE_NAMES:
                if name in parsed_cookie and parsed_cookie[name].value:
                    session_cookie = f"{name}={parsed_cookie[name].value}"
                    break
            if session_cookie:
                break
        if not session_cookie:
            raise BootstrapError("登录响应缺少 PeerGo 会话 cookie。")
        self.csrf_token = csrf_token
        self.session_cookie = session_cookie

    def logout(self) -> None:
        if not self.session_cookie or not self.csrf_token:
            return
        try:
            self.call("DELETE", "/api/v1/session", csrf=True)
        except BootstrapError:
            pass
        finally:
            self.session_cookie = ""
            self.csrf_token = ""


def wait_until_effective(client: ApiClient, deadline: datetime, poll_seconds: float) -> None:
    last_status = ""
    while True:
        newcomer_page, _ = client.call("GET", "/api/v1/admin/newcomer/policies?limit=20&offset=0")
        hnr_page, _ = client.call("GET", "/api/v1/admin/settings/hnr?limit=20&offset=0")
        newcomer_is_ready = newcomer_ready(newcomer_page)
        hnr_is_ready = hnr_ready(hnr_page)
        delivery_states = sorted(
            {
                str(item.get("delivery_state", "unknown"))
                for item in hnr_page.get("items", [])
                if isinstance(item, dict)
            }
        )
        status = (
            f"新人考核={'已生效' if newcomer_is_ready else '等待生效'}，"
            f"H&R={'已生效' if hnr_is_ready else '等待生效'}，"
            f"H&R投递={','.join(delivery_states) or '无'}"
        )
        if status != last_status:
            print(f"PeerGo production policies: {status}", flush=True)
            last_status = status
        if newcomer_is_ready and hnr_is_ready:
            return
        now = datetime.now(timezone.utc)
        if now >= deadline:
            raise BootstrapError("等待策略生效超时；保留已签发版本，请检查 core-policy-worker 后重试本命令。")
        time.sleep(min(poll_seconds, max(0.1, (deadline - now).total_seconds())))


def bootstrap(client: ApiClient, *, wait: bool, wait_timeout_seconds: int, poll_seconds: float) -> None:
    registration, _ = client.call("GET", "/api/v1/admin/settings/registration")
    if registration.get("mode") == "invite":
        print("PeerGo production policies: 注册模式已经是 invite，跳过。", flush=True)
    else:
        updated, _ = client.call(
            "PUT",
            "/api/v1/admin/settings/registration",
            registration_update_payload(registration),
            csrf=True,
        )
        if updated.get("mode") != "invite":
            raise BootstrapError("注册策略更新后仍不是 invite。")
        print("PeerGo production policies: 已启用 invite 注册；成员自行签发邀请保持原设置。", flush=True)

    newcomer_page, _ = client.call("GET", "/api/v1/admin/newcomer/policies?limit=20&offset=0")
    hnr_page, _ = client.call("GET", "/api/v1/admin/settings/hnr?limit=20&offset=0")
    newcomer_needs_issue = not newcomer_has_enabled_revision(newcomer_page)
    hnr_needs_issue = not hnr_has_revision(hnr_page)

    effective_at: datetime | None = None
    if newcomer_needs_issue or hnr_needs_issue:
        effective_at = choose_effective_at(
            datetime.now(timezone.utc),
            str(newcomer_page["minimum_effective_from"]),
            str(hnr_page["minimum_effective_from"]),
        )
        effective_text = format_rfc3339(effective_at)
        print(f"PeerGo production policies: 首版不可变策略将在 {effective_text} 生效。", flush=True)

        if newcomer_needs_issue:
            client.call(
                "POST",
                "/api/v1/admin/newcomer/policies",
                {
                    "policy": DEFAULT_NEWCOMER_POLICY,
                    "effective_at": effective_text,
                    "reason": "生产上线前启用首版新人考核规则。",
                },
                csrf=True,
                idempotency_key=str(uuid.uuid4()),
            )
            print("PeerGo production policies: 已签发新人考核（30 天 / 50 GiB / 72 小时做种）。", flush=True)
        else:
            print("PeerGo production policies: 已有启用中的新人考核版本，未覆盖。", flush=True)

        if hnr_needs_issue:
            client.call(
                "POST",
                "/api/v1/admin/settings/hnr",
                {
                    "policy": DEFAULT_HNR_POLICY,
                    "effective_at": effective_text,
                    "reason": "生产上线前签发首版全站 H&R 规则。",
                },
                csrf=True,
                idempotency_key=str(uuid.uuid4()),
            )
            print("PeerGo production policies: 已签发 H&R（7 天内做种 72 小时或分享率 1.0，宽限 24 小时）。", flush=True)
        else:
            print("PeerGo production policies: 已有 H&R 版本，未覆盖。", flush=True)
    else:
        print("PeerGo production policies: 新人考核与 H&R 均已有版本，未重复签发。", flush=True)

    if not wait:
        print("PeerGo production policies: 已按 --no-wait 返回；请在策略生效后运行 activation check。", flush=True)
        return

    if newcomer_ready(newcomer_page) and hnr_ready(hnr_page):
        print("PeerGo production policies: 新人考核与 H&R 已经生效。", flush=True)
        return

    now = datetime.now(timezone.utc)
    deadline = now + timedelta(seconds=wait_timeout_seconds)
    if effective_at is not None:
        deadline = max(deadline, effective_at + timedelta(minutes=2))
    wait_until_effective(client, deadline, poll_seconds)
    print("PeerGo production policies: 三项上线策略均已生效。", flush=True)


def parse_args(argv: list[str]) -> argparse.Namespace:
    repo_root = Path(__file__).resolve().parent.parent
    parser = argparse.ArgumentParser(description="签发 PeerGo 首版生产策略")
    parser.add_argument("--env-file", type=Path, default=repo_root / ".env.production")
    parser.add_argument("--username", required=True)
    parser.add_argument("--confirm", required=True)
    parser.add_argument("--no-wait", action="store_true")
    parser.add_argument("--wait-timeout-seconds", type=int, default=15 * 60)
    parser.add_argument("--poll-seconds", type=float, default=10.0)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv or sys.argv[1:])
    if args.confirm != CONFIRMATION:
        raise BootstrapError(f"确认值必须是 {CONFIRMATION}。")
    if not args.username.strip():
        raise BootstrapError("管理员用户名不能为空。")
    if args.wait_timeout_seconds < 60 or args.wait_timeout_seconds > 3600:
        raise BootstrapError("等待超时必须介于 60–3600 秒。")
    if args.poll_seconds < 1 or args.poll_seconds > 60:
        raise BootstrapError("轮询间隔必须介于 1–60 秒。")

    env = load_env_file(args.env_file)
    endpoint, public_origin = resolve_runtime(env)
    password = getpass.getpass(f"PeerGo 管理员 {args.username} 的密码：")
    if not password:
        raise BootstrapError("管理员密码不能为空。")

    client = ApiClient(endpoint=endpoint, public_origin=public_origin)
    try:
        client.login(
            args.username.strip(),
            password,
            lambda: getpass.getpass("两步验证码或一次性恢复码："),
        )
        password = ""
        print(f"PeerGo production policies: 已通过 {endpoint} 建立临时管理员会话。", flush=True)
        bootstrap(
            client,
            wait=not args.no_wait,
            wait_timeout_seconds=args.wait_timeout_seconds,
            poll_seconds=args.poll_seconds,
        )
    finally:
        password = ""
        client.logout()
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (BootstrapError, KeyboardInterrupt) as exc:
        message = "操作已中止。" if isinstance(exc, KeyboardInterrupt) else str(exc)
        print(f"PeerGo production policies: {message}", file=sys.stderr)
        raise SystemExit(1)
