#!/bin/sh
# Renders the looping demo clips that MediaMTX serves (see mediamtx.yml).
# Usage: make-clips.sh <output-dir>
#
# The clips are synthetic FFmpeg test sources, so there is nothing to download
# and no licensing to worry about. They are H.264 with a keyframe every second
# and no B-frames, which keeps the browser's latency to about one second.
set -eu

out=${1:?usage: make-clips.sh <output-dir>}
mkdir -p "$out"

encode() {
	name=$1
	source=$2
	ffmpeg -hide_banner -loglevel error -y -f lavfi -i "$source" -t 20 \
		-c:v libx264 -preset medium -profile:v main -pix_fmt yuv420p \
		-g 25 -keyint_min 25 -sc_threshold 0 -bf 0 \
		-b:v 1200k -maxrate 1500k -bufsize 1500k \
		"$out/$name.mp4"
	echo "rendered $out/$name.mp4"
}

encode testsrc "testsrc2=size=960x540:rate=25"
encode mandelbrot "mandelbrot=size=960x540:rate=25"
encode life "life=size=480x270:rate=25:mold=10:ratio=0.12:death_color=#1b2a4a:life_color=#5eead4,scale=960:540:flags=neighbor"
encode gradients "gradients=size=960x540:rate=25:speed=0.015:n=5"
