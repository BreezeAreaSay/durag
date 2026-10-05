#!/usr/bin/env bash
# One-time bootstrap of a fresh Ubuntu 22.04/24.04 or Debian 12 server:
# Docker Engine + the compose plugin, firewall (80/443/SSH), docker group.
#
#   sudo ./deploy/server-setup.sh
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "run as root (sudo ./deploy/server-setup.sh)"; exit 1
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y ca-certificates curl git

if ! command -v docker >/dev/null 2>&1; then
  echo "### Installing Docker Engine ..."
  curl -fsSL https://get.docker.com | sh
fi
if ! docker compose version >/dev/null 2>&1; then
  apt-get install -y docker-compose-plugin
fi
systemctl enable --now docker

if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
  usermod -aG docker "$SUDO_USER"
  echo "### $SUDO_USER added to the docker group (re-login to apply)"
fi

if command -v ufw >/dev/null 2>&1; then
  echo "### Opening SSH, 80 and 443 in ufw ..."
  ufw allow OpenSSH >/dev/null
  ufw allow 80/tcp >/dev/null
  ufw allow 443/tcp >/dev/null
  ufw --force enable >/dev/null
  ufw status | head -10
fi

echo
echo "Docker:  $(docker --version)"
echo "Compose: $(docker compose version)"
echo
echo "Next steps:"
echo "  cp .env.example .env && nano .env      # TELEGRAM_BOT_TOKEN, DOMAIN, CERTBOT_EMAIL, VITE_*"
echo "  ./deploy/init-letsencrypt.sh           # first certificate (DNS must already point here)"
echo "  ./deploy/deploy.sh                     # build + start + health check"
