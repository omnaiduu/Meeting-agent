#!/bin/sh
# Start a virtual screen for Chrome (Meet blocks headless Chrome), then the server.
set -e
if [ -z "$DISPLAY" ] && [ "$CHROME_HEADLESS" != "true" ]; then
  Xvfb :99 -screen 0 1280x720x24 -nolisten tcp >/tmp/xvfb.log 2>&1 &
  export DISPLAY=:99
  i=0
  while [ ! -e /tmp/.X11-unix/X99 ] && [ $i -lt 50 ]; do sleep 0.1; i=$((i + 1)); done
fi
exec /usr/local/bin/meeting-agent "$@"
