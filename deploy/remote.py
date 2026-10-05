#!/usr/bin/env python3
"""
Remote deployment driver for DURAG. Runs on the operator's machine (or an AI
agent's sandbox) and performs the whole production setup over SSH.

Credentials and settings come from environment variables only (never from
arguments, so nothing secret ends up in shell history or logs):

  DEPLOY_HOST            server IP or host name                      (required)
  DEPLOY_USER            ssh user, root or a sudo-capable user       (default: root)
  DEPLOY_PORT            ssh port                                     (default: 22)
  DEPLOY_PASSWORD        ssh password            -- or --
  DEPLOY_SSH_KEY         private key text (OpenSSH / PEM)  -- or --
  DEPLOY_SSH_KEY_PATH    path to a private key file
  DEPLOY_PATH            checkout path on the server                  (default: /opt/durag)
  DEPLOY_REPO            git URL                                      (default: https://github.com/BreezeAreaSay/durag.git)
  DEPLOY_BRANCH          branch to deploy                             (default: claude/ecstatic-maxwell-h3azwt)

  DOMAIN                 public domain pointing at the server         (required for bootstrap)
  CERTBOT_EMAIL          Let's Encrypt e-mail                         (required for bootstrap)
  TELEGRAM_BOT_TOKEN     bot token from @BotFather                    (required for bootstrap)
  VITE_BOT_USERNAME      bot username without @                       (recommended)
  VITE_APP_SHORTNAME     Mini App short name from /newapp             (recommended)
  LE_STAGING=1           use the Let's Encrypt staging CA (tests only)

Commands:
  python3 deploy/remote.py check        connectivity, docker, public IP vs DNS
  python3 deploy/remote.py bootstrap    full first deployment (idempotent, safe to re-run)
  python3 deploy/remote.py update       git pull + rebuild + restart + health check
  python3 deploy/remote.py status       docker compose ps + health endpoints
  python3 deploy/remote.py logs [svc]   last 120 log lines (default: backend)
  python3 deploy/remote.py run "<cmd>"  arbitrary command in the checkout directory

Requires: pip install paramiko
"""
from __future__ import annotations

import io
import os
import shlex
import socket
import sys
import time
import urllib.request

try:
    import paramiko
except ImportError:  # pragma: no cover
    sys.stderr.write("paramiko is missing: pip install paramiko\n")
    sys.exit(2)

REPO_DEFAULT = "https://github.com/BreezeAreaSay/durag.git"
BRANCH_DEFAULT = "claude/ecstatic-maxwell-h3azwt"


def env(name: str, default: str | None = None, required: bool = False) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        if required:
            sys.stderr.write(f"missing environment variable {name}\n")
            sys.exit(2)
        return default or ""
    return value


def mask(secret: str) -> str:
    if len(secret) <= 6:
        return "***"
    return secret[:3] + "…" + secret[-3:]


class Remote:
    def __init__(self) -> None:
        self.host = env("DEPLOY_HOST", required=True)
        self.user = env("DEPLOY_USER", "root")
        self.port = int(env("DEPLOY_PORT", "22"))
        self.password = env("DEPLOY_PASSWORD")
        self.path = env("DEPLOY_PATH", "/opt/durag")
        self.repo = env("DEPLOY_REPO", REPO_DEFAULT)
        self.branch = env("DEPLOY_BRANCH", BRANCH_DEFAULT)
        self.client = paramiko.SSHClient()
        self.client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        key = self._load_key()
        kwargs: dict = {"hostname": self.host, "port": self.port, "username": self.user, "timeout": 25, "banner_timeout": 30}
        if key is not None:
            kwargs["pkey"] = key
        elif self.password:
            kwargs["password"] = self.password
            kwargs["look_for_keys"] = False
            kwargs["allow_agent"] = False
        else:
            sys.stderr.write("set DEPLOY_PASSWORD or DEPLOY_SSH_KEY / DEPLOY_SSH_KEY_PATH\n")
            sys.exit(2)
        self.client.connect(**kwargs)

    def _load_key(self):
        text = os.environ.get("DEPLOY_SSH_KEY", "")
        path = env("DEPLOY_SSH_KEY_PATH")
        if not text and path:
            with open(os.path.expanduser(path), encoding="utf-8") as f:
                text = f.read()
        if not text.strip():
            return None
        text = text.replace("\\n", "\n").strip() + "\n"
        for cls in (paramiko.Ed25519Key, paramiko.RSAKey, paramiko.ECDSAKey):
            try:
                return cls.from_private_key(io.StringIO(text))
            except Exception:  # noqa: BLE001 - try the next key type
                continue
        sys.stderr.write("could not parse the private key (ed25519 / rsa / ecdsa supported)\n")
        sys.exit(2)

    # --- execution ---------------------------------------------------------

    def sudo(self, cmd: str) -> str:
        """Wraps a command so it runs as root (no-op for the root user)."""
        if self.user == "root":
            return cmd
        wrapped = f"bash -lc {shlex.quote(cmd)}"
        if self.password:
            return f"sudo -S -p '' {wrapped}"
        return f"sudo -n {wrapped}"

    def run(self, cmd: str, *, as_root: bool = False, check: bool = True, quiet: bool = False) -> tuple[int, str]:
        full = self.sudo(cmd) if as_root else cmd
        if not quiet:
            print(f"$ {cmd}", flush=True)
        stdin, stdout, stderr = self.client.exec_command(full, get_pty=False, timeout=1800)
        if as_root and self.user != "root" and self.password:
            stdin.write(self.password + "\n")
            stdin.flush()
        chunks: list[str] = []
        for line in iter(stdout.readline, ""):
            chunks.append(line)
            if not quiet:
                print("  " + line.rstrip(), flush=True)
        err = stderr.read().decode(errors="replace")
        code = stdout.channel.recv_exit_status()
        if err.strip() and not quiet:
            print("  [stderr] " + err.strip().replace("\n", "\n  [stderr] "), flush=True)
        if check and code != 0:
            raise SystemExit(f"command failed with exit code {code}: {cmd}")
        return code, "".join(chunks)

    def put_text(self, text: str, remote_path: str, mode: int = 0o600) -> None:
        sftp = self.client.open_sftp()
        try:
            with sftp.file(remote_path, "w") as f:
                f.write(text)
            sftp.chmod(remote_path, mode)
        finally:
            sftp.close()


# --- helpers -------------------------------------------------------------------


def public_ip(remote: Remote) -> str:
    _, out = remote.run("curl -4fsS --max-time 10 https://api.ipify.org || curl -4fsS --max-time 10 https://ifconfig.me", quiet=True, check=False)
    return out.strip().splitlines()[-1] if out.strip() else ""


def resolve(domain: str) -> list[str]:
    try:
        return sorted({info[4][0] for info in socket.getaddrinfo(domain, None, socket.AF_INET)})
    except socket.gaierror:
        return []


def http_get(url: str, timeout: int = 10) -> tuple[int, str]:
    try:
        with urllib.request.urlopen(url, timeout=timeout) as r:  # noqa: S310 - our own domain
            return r.status, r.read().decode(errors="replace")[:300]
    except Exception as exc:  # noqa: BLE001
        return 0, str(exc)


def dotenv_text() -> str:
    domain = env("DOMAIN", required=True)
    email = env("CERTBOT_EMAIL", required=True)
    token = env("TELEGRAM_BOT_TOKEN", required=True)
    lines = [
        "# generated by deploy/remote.py",
        f"TELEGRAM_BOT_TOKEN={token}",
        "ALLOW_DEV_AUTH=false",
        "PORT=8080",
        "REDIS_ADDR=redis:6379",
        "REDIS_PASSWORD=",
        "REDIS_DB=0",
        f"ALLOWED_ORIGINS=https://{domain}",
        "ROOM_TTL=24h",
        "TG_INITDATA_MAX_AGE=24h",
        f"BOUT_RESOLVE_DELAY={env('BOUT_RESOLVE_DELAY', '3500ms')}",
        f"DOMAIN={domain}",
        f"CERTBOT_EMAIL={email}",
        f"VITE_BOT_USERNAME={env('VITE_BOT_USERNAME')}",
        f"VITE_APP_SHORTNAME={env('VITE_APP_SHORTNAME')}",
    ]
    return "\n".join(lines) + "\n"


# --- commands -------------------------------------------------------------------


def cmd_check(remote: Remote) -> None:
    print(f"ssh ok: {remote.user}@{remote.host}:{remote.port}")
    remote.run("uname -a && (docker --version || echo 'docker: not installed') && (docker compose version || echo 'compose: not installed')", check=False)
    ip = public_ip(remote)
    print(f"server public IP: {ip or 'unknown'}")
    domain = env("DOMAIN")
    if domain:
        ips = resolve(domain)
        print(f"DNS {domain} -> {', '.join(ips) or 'does not resolve'}")
        if ip and ips and ip not in ips:
            print("WARNING: the domain does not point at this server yet; Let's Encrypt will fail until it does")
    print(f"token: {mask(env('TELEGRAM_BOT_TOKEN')) if env('TELEGRAM_BOT_TOKEN') else 'not set'}")


def ensure_checkout(remote: Remote) -> None:
    remote.run("command -v git >/dev/null 2>&1 || (apt-get update -y && apt-get install -y git)", as_root=True)
    remote.run(f"mkdir -p {shlex.quote(remote.path)} && chown {remote.user}:{remote.user} {shlex.quote(remote.path)}", as_root=True)
    code, _ = remote.run(f"test -d {shlex.quote(remote.path)}/.git", check=False, quiet=True)
    if code != 0:
        remote.run(f"git clone --branch {shlex.quote(remote.branch)} {shlex.quote(remote.repo)} {shlex.quote(remote.path)}")
    else:
        remote.run(f"cd {shlex.quote(remote.path)} && git fetch --all --prune && git checkout {shlex.quote(remote.branch)} && git pull --ff-only")


def cmd_bootstrap(remote: Remote) -> None:
    domain = env("DOMAIN", required=True)
    env("CERTBOT_EMAIL", required=True)
    env("TELEGRAM_BOT_TOKEN", required=True)

    print("### 1/6 checkout")
    ensure_checkout(remote)

    print("### 2/6 docker + firewall (server-setup.sh)")
    remote.run(f"cd {shlex.quote(remote.path)} && bash deploy/server-setup.sh", as_root=True)

    print("### 3/6 .env (secrets are not echoed)")
    remote.put_text(dotenv_text(), f"{remote.path}/.env")
    print("  written: .env")

    print("### 4/6 DNS")
    ip = public_ip(remote)
    for attempt in range(1, 7):
        ips = resolve(domain)
        if ip and ip in ips:
            print(f"  {domain} -> {ip}: ok")
            break
        print(f"  {domain} -> {', '.join(ips) or 'unresolved'}, server is {ip or 'unknown'}; waiting for DNS ({attempt}/6)")
        time.sleep(20)
    else:
        raise SystemExit("DNS does not point at the server; fix the A record and re-run bootstrap")

    print("### 5/6 certificate")
    code, _ = remote.run(f"test -f {shlex.quote(remote.path)}/deploy/certbot/conf/live/{shlex.quote(domain)}/fullchain.pem", check=False, quiet=True)
    if code != 0:
        staging = "STAGING=1 " if env("LE_STAGING") == "1" else ""
        remote.run(f"cd {shlex.quote(remote.path)} && {staging}bash deploy/init-letsencrypt.sh", as_root=True)
    else:
        print("  certificate already present")

    print("### 6/6 build and start")
    remote.run(f"cd {shlex.quote(remote.path)} && bash deploy/deploy.sh", as_root=True)
    verify(domain)


def cmd_update(remote: Remote) -> None:
    ensure_checkout(remote)
    remote.run(f"cd {shlex.quote(remote.path)} && bash deploy/deploy.sh", as_root=True)
    domain = env("DOMAIN")
    if not domain:
        _, out = remote.run(f"grep -E '^DOMAIN=' {shlex.quote(remote.path)}/.env | cut -d= -f2", quiet=True, check=False)
        domain = out.strip()
    if domain:
        verify(domain)


def verify(domain: str) -> None:
    print("### verification from here")
    for path in ("/healthz", "/api/config"):
        status, body = http_get(f"https://{domain}{path}")
        print(f"  GET https://{domain}{path} -> {status} {body.strip()[:120]}")
    status, _ = http_get(f"https://{domain}/")
    print(f"  GET https://{domain}/ -> {status}")
    print("Next (human): @BotFather /newapp -> Web App URL https://%s/ ; /setmenubutton -> same URL" % domain)


def cmd_status(remote: Remote) -> None:
    remote.run(f"cd {shlex.quote(remote.path)} && docker compose -f docker-compose.prod.yml ps", as_root=True, check=False)
    domain = env("DOMAIN")
    if domain:
        verify(domain)


def cmd_logs(remote: Remote, service: str) -> None:
    remote.run(f"cd {shlex.quote(remote.path)} && docker compose -f docker-compose.prod.yml logs --tail=120 {shlex.quote(service)}", as_root=True, check=False)


def main(argv: list[str]) -> None:
    if len(argv) < 2 or argv[1] in {"-h", "--help", "help"}:
        print(__doc__)
        return
    command = argv[1]
    remote = Remote()
    if command == "check":
        cmd_check(remote)
    elif command == "bootstrap":
        cmd_bootstrap(remote)
    elif command == "update":
        cmd_update(remote)
    elif command == "status":
        cmd_status(remote)
    elif command == "logs":
        cmd_logs(remote, argv[2] if len(argv) > 2 else "backend")
    elif command == "run":
        if len(argv) < 3:
            raise SystemExit("usage: remote.py run \"<command>\"")
        remote.run(f"cd {shlex.quote(remote.path)} && {argv[2]}", as_root=True, check=False)
    else:
        raise SystemExit(f"unknown command {command!r}; see --help")


if __name__ == "__main__":
    main(sys.argv)
