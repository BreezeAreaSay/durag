#!/bin/sh
# Executed by the nginx image entrypoint before nginx starts. Keeps a loop in
# the background that reloads nginx every 6 hours so that certificates renewed
# by the certbot container are picked up.
(
  while :; do
    sleep 6h
    nginx -s reload 2>/dev/null || true
  done
) &
