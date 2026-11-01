// Package media validates private uploads; no filename is used as a storage path.
package media

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	mp4 "github.com/abema/go-mp4"
	ebml "github.com/at-wat/ebml-go"
	"github.com/at-wat/ebml-go/webm"
	_ "golang.org/x/image/webp"
)

var ErrInvalid = errors.New("invalid media")
var ErrTooLarge = errors.New("media exceeds limits")
var ErrReferenced = errors.New("media is referenced")

const MaxPixels = 24_000_000

type Format struct {
	Kind   string
	MIME   string
	Width  int
	Height int
}

func Validate(body []byte, declared, purpose string) (out Format, err error) {
	defer func() {
		if recover() != nil {
			out = Format{}
			err = ErrInvalid
		}
	}()
	if len(body) == 0 {
		return out, ErrInvalid
	}
	if len(body) > 50<<20 {
		return out, ErrTooLarge
	}
	if purpose != "content" && purpose != "logo" && purpose != "favicon" && purpose != "portrait" && purpose != "banner" {
		return out, ErrInvalid
	}
	mime := http.DetectContentType(body)
	if mime == "image/jpeg" || mime == "image/png" || mime == "image/webp" {
		cfg, _, e := image.DecodeConfig(bytes.NewReader(body))
		if e != nil {
			return out, ErrInvalid
		}
		if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
			return out, ErrTooLarge
		}
		if len(body) > 10<<20 {
			return out, ErrTooLarge
		}
		if _, _, e = image.Decode(bytes.NewReader(body)); e != nil {
			return out, ErrInvalid
		}
		out = Format{"image", mime, cfg.Width, cfg.Height}
	} else if len(body) > 12 && string(body[4:8]) == "ftyp" {
		out, err = validateMP4(body)
	} else if bytes.HasPrefix(body, []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		out, err = validateWebM(body)
	} else if bytes.HasPrefix(body, []byte("%PDF-")) {
		if len(body) > 20<<20 {
			return out, ErrTooLarge
		}
		// PDF is download-only under a sandbox CSP. Require header, cross-reference
		// trailer and EOF rather than accepting a spoofed MIME/extension.
		if !regexp.MustCompile(`^%PDF-(1\.[0-7]|2\.0)[\r\n]`).Match(body) || !bytes.HasSuffix(bytes.TrimSpace(body), []byte("%%EOF")) {
			return out, ErrInvalid
		}
		i := bytes.LastIndex(body, []byte("startxref"))
		if i < 0 {
			return out, ErrInvalid
		}
		fields := strings.Fields(string(body[i+9:]))
		if len(fields) < 2 {
			return out, ErrInvalid
		}
		off, e := strconv.Atoi(fields[0])
		if e != nil || off < 9 || off >= i {
			return out, ErrInvalid
		}
		if !bytes.HasPrefix(body[off:], []byte("xref")) && !regexp.MustCompile(`^[0-9]+ [0-9]+ obj`).Match(body[off:]) {
			return out, ErrInvalid
		}
		out = Format{Kind: "pdf", MIME: "application/pdf"}
	} else if bytes.HasPrefix(body, []byte("WEBVTT")) {
		if len(body) > 1<<20 {
			return out, ErrTooLarge
		}
		if !validVTT(body) {
			return out, ErrInvalid
		}
		out = Format{Kind: "captions", MIME: "text/vtt"}
	} else {
		return out, ErrInvalid
	}
	if err != nil {
		return Format{}, err
	}
	if declared != "" && declared != "application/octet-stream" && declared != out.MIME {
		return Format{}, ErrInvalid
	}
	if err = CheckPurpose(out, int64(len(body)), purpose); err != nil {
		return Format{}, err
	}
	return out, nil
}
func CheckPurpose(f Format, size int64, p string) error {
	switch p {
	case "content":
		return nil
	case "logo", "favicon":
		if f.Kind != "image" || size > 1<<20 {
			return ErrInvalid
		}
	case "portrait":
		if f.Kind != "image" || size > 2<<20 || f.Width != f.Height || f.Width < 600 {
			return ErrInvalid
		}
	case "banner":
		if f.Kind != "image" || size > 10<<20 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

var cueTime = regexp.MustCompile(`^(?:(\d{2,}):)?(\d{2}):(\d{2})\.(\d{3})$`)

func timestamp(s string) (int64, bool) {
	m := cueTime.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	var a [4]int64
	for i := range a {
		if m[i+1] != "" {
			v, e := strconv.ParseInt(m[i+1], 10, 32)
			if e != nil {
				return 0, false
			}
			a[i] = v
		}
	}
	return ((a[0]*60+a[1])*60+a[2])*1000 + a[3], a[1] < 60 && a[2] < 60
}
func validVTT(b []byte) bool {
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	blocks := strings.Split(s, "\n\n")
	if len(blocks) < 2 || !(blocks[0] == "WEBVTT" || strings.HasPrefix(blocks[0], "WEBVTT ")) {
		return false
	}
	n := 0
	for _, block := range blocks[1:] {
		if strings.TrimSpace(block) == "" || strings.HasPrefix(block, "NOTE") {
			continue
		}
		lines := strings.Split(block, "\n")
		idx := 0
		if !strings.Contains(lines[0], " --> ") {
			idx = 1
		}
		if idx >= len(lines) {
			return false
		}
		p := strings.Fields(lines[idx])
		if len(p) < 3 || p[1] != "-->" {
			return false
		}
		start, ok := timestamp(p[0])
		end, ok2 := timestamp(p[2])
		if !ok || !ok2 || end <= start || idx+1 >= len(lines) {
			return false
		}
		n++
	}
	return n > 0
}
func validateMP4(b []byte) (Format, error) {
	out := Format{Kind: "video", MIME: "video/mp4"}
	ftyp, mdat, moov, avcc, video, audio := false, false, false, false, 0, 0
	aac := false
	count := 0
	_, e := mp4.ReadBoxStructure(bytes.NewReader(b), func(h *mp4.ReadHandle) (interface{}, error) {
		count++
		if count > 10000 || len(h.Path) > 12 || h.BoxInfo.Offset > uint64(len(b)) || h.BoxInfo.Size > uint64(len(b))-h.BoxInfo.Offset {
			return nil, ErrInvalid
		}
		typ := h.BoxInfo.Type.String()
		// Codec declarations only count inside a real track's sample description.
		// Reject misplaced known boxes rather than accepting detached codec headers.
		parents := map[string]string{
			"ftyp": "", "mdat": "", "moov": "", "trak": "moov",
			"mdia": "moov/trak", "minf": "moov/trak/mdia",
			"stbl": "moov/trak/mdia/minf", "stsd": "moov/trak/mdia/minf/stbl",
			"avc1": "moov/trak/mdia/minf/stbl/stsd", "mp4a": "moov/trak/mdia/minf/stbl/stsd",
			"avcC": "moov/trak/mdia/minf/stbl/stsd/avc1", "esds": "moov/trak/mdia/minf/stbl/stsd/mp4a",
		}
		if parent, known := parents[typ]; known {
			path := make([]string, len(h.Path)-1)
			for i := range path {
				path[i] = h.Path[i].String()
			}
			if strings.Join(path, "/") != parent {
				return nil, ErrInvalid
			}
		}
		if len(h.Path) >= 2 && h.Path[len(h.Path)-2].String() == "stsd" && typ != "avc1" && typ != "mp4a" {
			return nil, ErrInvalid
		}
		switch typ {
		case "ftyp":
			ftyp = true
		case "mdat":
			mdat = h.BoxInfo.Size > h.BoxInfo.HeaderSize
		case "moov":
			moov = true
			return h.Expand()
		case "trak", "mdia", "minf", "stbl", "stsd":
			return h.Expand()
		case "avc1":
			box, _, e := h.ReadPayload()
			if e != nil {
				return nil, e
			}
			v := box.(*mp4.VisualSampleEntry)
			out.Width = int(v.Width)
			out.Height = int(v.Height)
			video++
			return h.Expand()
		case "avcC":
			box, _, e := h.ReadPayload()
			if e != nil {
				return nil, e
			}
			v := box.(*mp4.AVCDecoderConfiguration)
			if v.ConfigurationVersion != 1 || len(v.SequenceParameterSets) == 0 || len(v.PictureParameterSets) == 0 {
				return nil, ErrInvalid
			}
			avcc = true
		case "mp4a":
			audio++
			return h.Expand()
		case "esds":
			box, _, e := h.ReadPayload()
			if e != nil {
				return nil, e
			}
			codec, config := false, false
			for _, desc := range box.(*mp4.Esds).Descriptors {
				if desc.DecoderConfigDescriptor != nil {
					codec = desc.DecoderConfigDescriptor.ObjectTypeIndication == 0x40 && desc.DecoderConfigDescriptor.StreamType == 5
				}
				if desc.Tag == mp4.DecSpecificInfoTag && len(desc.Data) >= 2 {
					config = desc.Data[0]>>3 == 2
				}
			}
			if !codec || !config {
				return nil, ErrInvalid
			}
			aac = true
		}
		return nil, nil
	})
	if e != nil || !ftyp || !mdat || !moov || !avcc || video != 1 || audio > 1 || (audio == 1 && !aac) || out.Width < 1 || out.Height < 1 || out.Width > 4096 || out.Height > 4096 {
		return Format{}, ErrInvalid
	}
	return out, nil
}
func validateWebM(b []byte) (Format, error) {
	var doc struct {
		EBML    webm.EBMLHeader
		Segment webm.Segment
	}
	e := ebml.Unmarshal(bytes.NewReader(b), &doc, ebml.WithMaxLeafElementSize(50<<20))
	if e != nil || doc.EBML.DocType != "webm" || len(doc.Segment.Tracks.TrackEntry) > 2 {
		return Format{}, ErrInvalid
	}
	out := Format{Kind: "video", MIME: "video/webm"}
	tracks := map[uint64]bool{}
	videos := 0
	for _, t := range doc.Segment.Tracks.TrackEntry {
		if t.TrackNumber == 0 || tracks[t.TrackNumber] {
			return Format{}, ErrInvalid
		}
		tracks[t.TrackNumber] = true
		switch t.TrackType {
		case 1:
			if t.CodecID != "V_VP8" && t.CodecID != "V_VP9" {
				return Format{}, ErrInvalid
			}
			if t.Video == nil || t.Video.PixelWidth < 1 || t.Video.PixelHeight < 1 || t.Video.PixelWidth > 4096 || t.Video.PixelHeight > 4096 {
				return Format{}, ErrInvalid
			}
			out.Width = int(t.Video.PixelWidth)
			out.Height = int(t.Video.PixelHeight)
			videos++
		case 2:
			if t.CodecID != "A_OPUS" && t.CodecID != "A_VORBIS" {
				return Format{}, ErrInvalid
			}
		default:
			return Format{}, ErrInvalid
		}
	}
	blocks := 0
	for _, c := range doc.Segment.Cluster {
		for _, v := range c.SimpleBlock {
			if !tracks[v.TrackNumber] || len(v.Data) == 0 {
				return Format{}, ErrInvalid
			}
			blocks++
		}
		for _, v := range c.BlockGroup {
			if !tracks[v.Block.TrackNumber] || len(v.Block.Data) == 0 {
				return Format{}, ErrInvalid
			}
			blocks++
		}
	}
	if videos != 1 || blocks == 0 {
		return Format{}, ErrInvalid
	}
	return out, nil
}
