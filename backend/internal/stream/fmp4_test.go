package stream

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"
)

// Both fixtures were produced by FFmpeg with the same -movflags the backend
// uses: 3s of H.264 with a keyframe every second, and 1s of HEVC.
func TestReadSegmentsFFmpegOutput(t *testing.T) {
	data, err := os.ReadFile("testdata/h264-3s.mp4")
	if err != nil {
		t.Fatal(err)
	}

	var segs []Segment
	err = readSegments(bytes.NewReader(data), func(s Segment) error {
		segs = append(segs, s)
		return nil
	})
	if err != nil {
		t.Fatalf("readSegments: %v", err)
	}

	if len(segs) != 4 {
		t.Fatalf("got %d segments, want init + 3 one-second fragments", len(segs))
	}
	if !segs[0].Init || segs[0].Codec != "avc1.64000b" { // High profile, level 1.1
		t.Errorf("first segment: init=%v codec=%q, want init with avc1.64000b", segs[0].Init, segs[0].Codec)
	}
	total := 0
	for i, s := range segs {
		total += len(s.Data)
		if i > 0 && (s.Init || string(s.Data[4:8]) != "moof") {
			t.Errorf("segment %d should be a fragment starting with moof, got %q", i, s.Data[4:8])
		}
	}
	// FFmpeg closes the file with an mfra index box, which carries no media.
	if trailer := data[total:]; string(trailer[4:8]) != "mfra" {
		t.Errorf("segments hold %d of %d bytes and the rest is %q, want only mfra left over", total, len(data), trailer[4:8])
	}
}

func TestCodecStringHEVC(t *testing.T) {
	data, err := os.ReadFile("testdata/hevc-1s.mp4")
	if err != nil {
		t.Fatal(err)
	}
	var codec string
	_ = readSegments(bytes.NewReader(data), func(s Segment) error {
		if s.Init {
			codec = s.Codec
		}
		return nil
	})
	if codec != "hvc1" || isH264(codec) {
		t.Errorf("codec = %q, want hvc1 and not H.264", codec)
	}
}

func box(typ string, payload []byte) []byte {
	b := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(b, uint32(8+len(payload)))
	copy(b[4:], typ)
	return append(b, payload...)
}

func TestReadSegmentsLargeSizeBox(t *testing.T) {
	payload := []byte("media")
	large := make([]byte, 16)
	binary.BigEndian.PutUint32(large, 1)
	copy(large[4:], "mdat")
	binary.BigEndian.PutUint64(large[8:], uint64(16+len(payload)))
	large = append(large, payload...)

	stream := append(box("moof", nil), large...)
	var got []Segment
	if err := readSegments(bytes.NewReader(stream), func(s Segment) error {
		got = append(got, s)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !bytes.Equal(got[0].Data, stream) {
		t.Fatalf("got %d segments, want one fragment equal to the input", len(got))
	}
}

func TestReadSegmentsErrors(t *testing.T) {
	tooBig := make([]byte, 8)
	binary.BigEndian.PutUint32(tooBig, maxBoxSize+1)
	copy(tooBig[4:], "mdat")

	cases := map[string]struct {
		input []byte
		want  string
	}{
		"truncated header":  {[]byte{0, 0, 0}, "truncated mp4 box header"},
		"truncated body":    {box("moof", make([]byte, 10))[:12], "truncated mp4 box"},
		"size below header": {[]byte{0, 0, 0, 4, 'm', 'o', 'o', 'f'}, "invalid size"},
		"oversized box":     {tooBig, "exceeds size limit"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := readSegments(bytes.NewReader(tc.input), func(Segment) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestReadSegmentsStopsOnEmitError(t *testing.T) {
	stop := errors.New("stop")
	stream := append(box("moof", nil), box("mdat", nil)...)
	stream = append(stream, stream...)
	calls := 0
	err := readSegments(bytes.NewReader(stream), func(Segment) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Fatalf("err = %v after %d calls, want stop after 1", err, calls)
	}
}
