package stream

import (
	"slices"
	"strings"
	"testing"
)

func TestDescribeFailure(t *testing.T) {
	cases := map[string]string{
		"[rtsp @ 0x1] method DESCRIBE failed: 401 Unauthorized":                                        "Authentication failed",
		"[rtsp @ 0x1] method DESCRIBE failed: 404 Not Found":                                           "Stream not found",
		"[tcp @ 0x1] Connection to tcp://cam:554?timeout=10000000 failed: Connection refused":          "Connection refused",
		"[tcp @ 0x1] Connection to tcp://cam:554?timeout=10000000 failed: Connection timed out":        "Timed out",
		"[tcp @ 0x1] Connection to tcp://cam:554?timeout=10000000 failed: Error number -138 occurred":  "Timed out",
		"[tcp @ 0x1] Failed to resolve hostname cam.invalid: Name or service not known":                "Unknown host",
		"[tcp @ 0x1] Connection to tcp://10.0.0.1:554?timeout=10000000 failed: Network is unreachable": "Network unreachable",
		"rtsp://cam/x: Invalid data found when processing input":                                       "did not send a stream",
		"something new and unexpected\n":                                                               "something new and unexpected",
		"":                                                                                             "Stream ended",
	}
	for stderr, want := range cases {
		if got := describeFailure(stderr, nil); !strings.Contains(got, want) {
			t.Errorf("describeFailure(%q) = %q, want it to contain %q", stderr, got, want)
		}
	}
}

func TestArgs(t *testing.T) {
	f := &FFmpeg{RTSPTransport: "tcp"}

	copyArgs := f.Args(RunOptions{URL: "rtsp://cam/live"})
	if !slices.Contains(copyArgs, "copy") || slices.Contains(copyArgs, "libx264") {
		t.Errorf("copy mode args: %v", copyArgs)
	}
	i := slices.Index(copyArgs, "-i")
	if i < 0 || copyArgs[i+1] != "rtsp://cam/live" {
		t.Errorf("URL should follow -i as its own argument: %v", copyArgs)
	}
	if copyArgs[len(copyArgs)-1] != "pipe:1" {
		t.Errorf("output should go to stdout: %v", copyArgs)
	}

	transcodeArgs := f.Args(RunOptions{URL: "rtsp://cam/live", Transcode: true})
	if !slices.Contains(transcodeArgs, "libx264") || slices.Contains(transcodeArgs, "copy") {
		t.Errorf("transcode mode args: %v", transcodeArgs)
	}
}
