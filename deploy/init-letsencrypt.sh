#!/usr/bin/env bash
# One-off bootstrap of Let's Encrypt certificates for docker-compose.prod.yml.
# Nginx refuses to start without a certificate, so we create a throw-away
# self-signed one, start Nginx, obtain the real certificate through the
# webroot challenge and reload.
#
#   ./deploy/init-letsencrypt.sh            # uses DOMAIN / CERTBOT_EMAIL from .env
#   STAGING=1 ./deploy/init-letsencrypt.sh  # Let's Encrypt staging (no rate limits)
set -euo pipefail

cd "$(dirname "$0")/.."
if [ -f .env ]; then
  # shellcheck disable=SC2046
  export $(grep -E '^(DOMAIN|CERTBOT_EMAIL)=' .env | xargs)
fi
: "${DOMAIN:?set DOMAIN in .env}"
: "${CERTBOT_EMAIL:?set CERTBOT_EMAIL in .env}"
STAGING="${STAGING:-0}"
COMPOSE="docker compose -f docker-compose.prod.yml"
CONF="./deploy/certbot/conf"
LIVE="$CONF/live/$DOMAIN"

if [ -d "$LIVE" ] && [ "${FORCE:-0}" != "1" ]; then
  echo "Certificate for $DOMAIN already exists in $LIVE (FORCE=1 to redo)."
  exit 0
fi

echo "### Creating a dummy certificate for $DOMAIN ..."
mkdir -p "$LIVE"
$COMPOSE run --rm --entrypoint "\
  openssl req -x509 -nodes -newkey rsa:2048 -days 1 \
    -keyout '/etc/letsencrypt/live/$DOMAIN/privkey.pem' \
    -out '/etc/letsencrypt/live/$DOMAIN/fullchain.pem' \
    -subj '/CN=localhost'" certbot

echo "### Starting nginx ..."
$COMPOSE up --force-recreate -d nginx

echo "### Deleting the dummy certificate ..."
$COMPOSE run --rm --entrypoint "\
  rm -rf /etc/letsencrypt/live/$DOMAIN /etc/letsencrypt/archive/$DOMAIN /etc/letsencrypt/renewal/$DOMAIN.conf" certbot

echo "### Requesting the Let's Encrypt certificate for $DOMAIN ..."
STAGING_ARG=""
if [ "$STAGING" = "1" ]; then STAGING_ARG="--staging"; fi
$COMPOSE run --rm --entrypoint "\
  certbot certonly --webroot -w /var/www/certbot \
    $STAGING_ARG \
    --email '$CERTBOT_EMAIL' \
    -d '$DOMAIN' \
    --rsa-key-size 4096 \
    --agree-tos \
    --no-eff-email \
    --force-renewal" certbot

echo "### Reloading nginx ..."
$COMPOSE exec nginx nginx -s reload
echo "Done. Start everything with: $COMPOSE up -d --build"
