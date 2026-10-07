#!/bin/sh
# Container entrypoint: starts the demo RTSP server (unless DEMO_SOURCES=off)
# and then the backend in the foreground.
set -eu

if [ "${DEMO_SOURCES:-on}" != "off" ]; then
	# Keep MediaMTX running so the demo streams come back if it ever exits.
	(
		while true; do
			mediamtx /etc/mediamtx.yml || true
			echo "mediamtx exited, restarting in 1s" >&2
			sleep 1
		done
	) &
else
	unset DEMO_STREAMS
fi

exec server
