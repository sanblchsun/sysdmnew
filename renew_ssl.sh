#!/bin/bash
# Script for automatic SSL certificate renewal using Docker Compose

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Use script directory as WORKDIR
WORKDIR="$SCRIPT_DIR"

# Other paths relative to WORKDIR
LOG="$WORKDIR/renew_ssl.log"
DEBUG_LOG="$WORKDIR/renew_debug.log"
COMPOSE="/usr/local/bin/docker-compose"

echo "[`date`] Starting certificate renewal" >> "$DEBUG_LOG"
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

cd "$WORKDIR"

echo "[INFO] `date`: Starting certbot renew" >> "$LOG"
$COMPOSE run --rm certbot renew >> "$LOG" 2>&1

echo "[INFO] `date`: Reloading nginx" >> "$LOG"
$COMPOSE exec nginx nginx -s reload >> "$LOG" 2>&1

echo "[INFO] `date`: Done" >> "$LOG"
echo "[`date`] Done" >> "$DEBUG_LOG"
