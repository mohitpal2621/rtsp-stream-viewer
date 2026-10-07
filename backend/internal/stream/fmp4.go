package stream

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// FFmpeg is asked for fragmented MP4 with one fragment per keyframe
// (-movflags frag_keyframe+empty_moov). The byte stream is then:
//
//	ftyp moov | moof mdat | moof mdat | ...
//
// ftyp+moov is the initialisation segment every viewer needs first. Each
// moof+mdat pair is a media fragment that starts on a keyframe, so a viewer
// can join, or skip fragments when it falls behind, at any fragment boundary.

// Segment is one piece of the fragmented MP4 stream.
type Segment struct {
	Init  bool   // true for the ftyp+moov initialisation segment
	Data  []byte // complete boxes, ready to append to a browser SourceBuffer
	Codec string // RFC 6381 codec string, set on init segments only
}

const maxBoxSize = 32 << 20 // a single GOP larger than this means something is wrong

var errBoxTooLarge = errors.New("mp4 box exceeds size limit")

// readSegments reads a fragmented MP4 stream from r and calls emit for the
// init segment and for every media fragment. It returns nil at a clean EOF
// between boxes, or the first error from reading or from emit.
func readSegments(r io.Reader, emit func(Segment) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var pending []byte // boxes collected for the segment being built
	for {
		box, typ, err := readBox(br)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		pending = append(pending, box...)

		switch typ {
		case "moov":
			if err := emit(Segment{Init: true, Data: pending, Codec: codecString(pending)}); err != nil {
				return err
			}
			pending = nil
		case "mdat":
			if err := emit(Segment{Data: pending}); err != nil {
				return err
			}
			pending = nil
		}
	}
}

// readBox reads one complete top-level box, header included.
func readBox(r io.Reader) ([]byte, string, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, "", fmt.Errorf("truncated mp4 box header: %w", err)
		}
		return nil, "", err
	}
	size := uint64(binary.BigEndian.Uint32(header[0:4]))
	typ := string(header[4:8])

	headerLen := uint64(8)
	if size == 1 { // 64-bit "largesize" follows the type
		ext := make([]byte, 8)
		if _, err := io.ReadFull(r, ext); err != nil {
			return nil, "", fmt.Errorf("truncated mp4 box header: %w", err)
		}
		header = append(header, ext...)
		size = binary.BigEndian.Uint64(ext)
		headerLen = 16
	}
	if size < headerLen {
		return nil, "", fmt.Errorf("invalid size %d for mp4 box %q", size, typ)
	}
	if size > maxBoxSize {
		return nil, "", fmt.Errorf("%w: %q is %d bytes", errBoxTooLarge, typ, size)
	}

	box := make([]byte, size)
	copy(box, header)
	if _, err := io.ReadFull(r, box[headerLen:]); err != nil {
		return nil, "", fmt.Errorf("truncated mp4 box %q: %w", typ, err)
	}
	return box, typ, nil
}

// childBox returns the payload of the first box of type typ in data, which
// must be a sequence of boxes. It returns nil if there is none.
func childBox(data []byte, typ string) []byte {
	for len(data) >= 8 {
		size := int(binary.BigEndian.Uint32(data[0:4]))
		if size < 8 || size > len(data) {
			return nil
		}
		if string(data[4:8]) == typ {
			return data[8:size]
		}
		data = data[size:]
	}
	return nil
}

// codecString extracts the codec of the first track from an init segment, in
// the form MediaSource.addSourceBuffer expects, e.g. "avc1.64001f". It returns
// the bare sample entry type (such as "hvc1") for codecs it cannot describe in
// full, and "" if the segment can't be parsed.
func codecString(init []byte) string {
	stbl := init
	for _, typ := range []string{"moov", "trak", "mdia", "minf", "stbl", "stsd"} {
		if stbl = childBox(stbl, typ); stbl == nil {
			return ""
		}
	}
	stsd := stbl
	// stsd is a full box: version+flags (4) and entry count (4), then entries.
	if len(stsd) < 16 {
		return ""
	}
	entry := stsd[8:]
	entrySize := int(binary.BigEndian.Uint32(entry[0:4]))
	sampleType := string(entry[4:8])
	if entrySize < 8 || entrySize > len(entry) {
		return sampleType
	}

	if sampleType != "avc1" && sampleType != "avc3" {
		return sampleType
	}
	// A visual sample entry has 78 bytes of fixed fields before its child boxes.
	const visualSampleEntryLen = 8 + 78
	if entrySize < visualSampleEntryLen {
		return sampleType
	}
	avcC := childBox(entry[visualSampleEntryLen:entrySize], "avcC")
	if len(avcC) < 4 {
		return sampleType
	}
	// avcC: configurationVersion, AVCProfileIndication, profile_compatibility, AVCLevelIndication
	return fmt.Sprintf("%s.%02x%02x%02x", sampleType, avcC[1], avcC[2], avcC[3])
}

// isH264 reports whether codec, as returned by codecString, is H.264, the one
// video codec every browser's MediaSource implementation can decode.
func isH264(codec string) bool {
	return len(codec) >= 4 && (codec[:4] == "avc1" || codec[:4] == "avc3")
}
