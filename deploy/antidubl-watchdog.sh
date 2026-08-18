#!/bin/bash
#
# Watchdog for antidubl.service — restarts the bot when its
# heartbeat file goes stale (process alive but hung). Crashes are
# already handled by Restart=on-failure in the service unit; this
# covers the stuck-but-running case.
#
# Fired every 2 minutes by antidubl-watchdog.timer.
#
HB=${ANTIDUBL_HEARTBEAT:-/opt/antidubl/heartbeat}
STATE=/var/lib/antidubl-watchdog/last-restart
LOG=/var/log/antidubl-watchdog.log
STALE=300    # heartbeat older than this -> restart
MIN_GAP=600  # at most one restart per 10 minutes

# Never touch a service the admin stopped on purpose (or one that
# already crashed — Restart=on-failure handles it).
systemctl is-active --quiet antidubl || exit 0

now=$(date +%s)

if [ -f "$HB" ]; then
  hb=$(cat "$HB" 2>/dev/null)
  case "$hb" in
    ''|*[!0-9]*) age=999999 ;;
    *) age=$(( now - hb )) ;;
  esac
else
  age=999999
fi

if [ "$age" -lt "$STALE" ]; then
  exit 0
fi

mkdir -p "$(dirname "$STATE")"
if [ -f "$STATE" ]; then
  last=$(cat "$STATE" 2>/dev/null)
  case "$last" in
    ''|*[!0-9]*) last=0 ;;
  esac
  if [ $(( now - last )) -lt "$MIN_GAP" ]; then
    echo "$(date -Is): heartbeat stale (${age}s), restart skipped (rate limit)" >> "$LOG"
    exit 0
  fi
fi

echo "$now" > "$STATE"
echo "$(date -Is): heartbeat stale (${age}s) -> restarting antidubl" >> "$LOG"
systemctl restart antidubl
