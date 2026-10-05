#!/usr/bin/env bash
# Build and (re)start the production stack, then wait until /healthz answers
# through nginx + TLS. Safe to re-run: this is also the update procedure.
#
#   ./deploy/deploy.sh            # build & up
#   ./deploy/deploy.sh --pull     # git pull --ff-only first
set -euo pipefail

cd "$(dirname "$0")/.."
COMPOSE="docker compose -f docker-compose.prod.yml"

if [ ! -f .env ]; then
  echo ".env is missing: cp .env.example .env and fill it in"; exit 1
fi
# shellcheck disable=SC2046
export $(grep -E '^(DOMAIN)=' .env | xargs)
: "${DOMAIN:?set DOMAIN in .env}"

if [ "${1:-}" = "--pull" ]; then
  git pull --ff-only
fi

if [ ! -f "deploy/certbot/conf/live/$DOMAIN/fullchain.pem" ]; then
  echo "No certificate for $DOMAIN yet. Run ./deploy/init-letsencrypt.sh first."; exit 1
fi

echo "### Building images ..."
$COMPOSE build --pull
echo "### Starting ..."
$COMPOSE up -d --remove-orphans
docker image prune -f >/dev/null 2>&1 || true

echo "### Waiting for https://$DOMAIN/healthz ..."
for _ in $(seq 1 40); do
  if curl -fsS -m 5 "https://$DOMAIN/healthz" >/dev/null 2>&1; then
    echo "### Healthy."
    $COMPOSE ps
    exit 0
  fi
  sleep 3
done
echo "### Health check FAILED. Recent logs:"
$COMPOSE ps
$COMPOSE logs --tail=60 nginx backend
exit 1
