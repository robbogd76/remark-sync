// Package rmparse parses Remarkable .rm binary files (v5 and v6 formats)
// into a slice of Strokes suitable for rendering.
//
// File format reference: https://github.com/ricklupton/rmscene
package rmparse

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Canvas dimensions in Remarkable coordinate units.
const (
	CanvasWidth  = 1404
	CanvasHeight = 1872
)

const (
	headerV5 = "reMarkable .lines file, version=5          "
	headerV6 = "reMarkable .lines file, version=6          "
	headerLen = 43

	// Eraser tool IDs — strokes using these are invisible.
	toolEraser     = 6
	toolEraserArea = 8
)

// Color IDs used in both v5 and v6.
const (
	ColorBlack = 0
	ColorGray  = 1
	ColorWhite = 2
)

// Stroke is one continuous pen stroke.
type Stroke struct {
	Color  int     // see Color* constants
	Tool   int     // pen tool identifier
	Width  float32 // base rendered width (v5 brush_base_size)
	Points []Point
}

// IsVisible reports whether this stroke should be drawn (not an eraser, not white).
func (s Stroke) IsVisible() bool {
	return s.Tool != toolEraser && s.Tool != toolEraserArea && s.Color != ColorWhite
}

// Point is a single sample within a stroke.
type Point struct {
	X        float32 // canvas x (0 – CanvasWidth)
	Y        float32 // canvas y (0 – CanvasHeight)
	Width    float32 // per-point width
	Pressure float32 // 0.0 – 1.0
}

// Parse reads a .rm file and returns all visible strokes.
func Parse(r io.Reader) ([]Stroke, error) {
	hdr := make([]byte, headerLen)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, fmt.Errorf("rm header: %w", err)
	}

	switch string(hdr) {
	case headerV5:
		return parseV5(r)
	case headerV6:
		rest, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("reading v6 body: %w", err)
		}
		return parseV6(rest)
	default:
		return nil, fmt.Errorf("unsupported .rm format (header: %q)", string(hdr[:10]))
	}
}

// ─────────────────────────────────────────────────────────────────── v5 ──────

func parseV5(r io.Reader) ([]Stroke, error) {
	var numLayers int32
	if err := binary.Read(r, binary.LittleEndian, &numLayers); err != nil {
		return nil, fmt.Errorf("v5 layer count: %w", err)
	}

	var all []Stroke
	for l := int32(0); l < numLayers; l++ {
		var numStrokes int32
		if err := binary.Read(r, binary.LittleEndian, &numStrokes); err != nil {
			return nil, fmt.Errorf("v5 layer %d stroke count: %w", l, err)
		}
		for s := int32(0); s < numStrokes; s++ {
			stroke, err := readV5Stroke(r)
			if err != nil {
				return nil, fmt.Errorf("v5 layer %d stroke %d: %w", l, s, err)
			}
			if stroke.IsVisible() && len(stroke.Points) > 0 {
				all = append(all, stroke)
			}
		}
	}
	return all, nil
}

// v5 stroke header — 24 bytes.
type v5StrokeHdr struct {
	BrushType int32
	Color     int32
	_         int32   // unknown / padding
	BrushSize float32 // base width in canvas units
	_         int32   // unknown
	NumPoints int32
}

func readV5Stroke(r io.Reader) (Stroke, error) {
	var h v5StrokeHdr
	if err := binary.Read(r, binary.LittleEndian, &h); err != nil {
		return Stroke{}, err
	}

	pts := make([]Point, h.NumPoints)
	for i := range pts {
		// v5 point: 6 × float32 = 24 bytes
		var p struct {
			X, Y, Speed, Direction, Width, Pressure float32
		}
		if err := binary.Read(r, binary.LittleEndian, &p); err != nil {
			return Stroke{}, fmt.Errorf("point %d: %w", i, err)
		}
		pts[i] = Point{X: p.X, Y: p.Y, Width: p.Width, Pressure: p.Pressure}
	}

	return Stroke{
		Color:  int(h.Color),
		Tool:   int(h.BrushType),
		Width:  h.BrushSize,
		Points: pts,
	}, nil
}

// ─────────────────────────────────────────────────────────────────── v6 ──────

// v6 block header — 8 bytes: length(4) | unknown(1) | minVer(1) | curVer(1) | type(1)
const (
	v6BlockHeaderLen    = 8
	v6BlockSceneLineItem = 0x05 // SceneLineItemBlock — contains one stroke

	// Tag type nibble values (lower 4 bits of the tag byte).
	tagByte1    = 0x1
	tagByte4    = 0x4
	tagByte8    = 0x8
	tagLength4  = 0xC // 4-byte length-prefixed subblock
	tagID       = 0xF // CrdtId: 1 byte + varint
)

func parseV6(data []byte) ([]Stroke, error) {
	var all []Stroke
	pos := 0

	for pos+v6BlockHeaderLen <= len(data) {
		length := int(binary.LittleEndian.Uint32(data[pos:]))
		blockType := int(data[pos+7])
		pos += v6BlockHeaderLen

		end := pos + length
		if end > len(data) {
			break
		}
		blockData := data[pos:end]
		pos = end

		if blockType == v6BlockSceneLineItem {
			stroke, err := parseV6LineBlock(blockData)
			if err != nil || stroke == nil {
				continue
			}
			if stroke.IsVisible() && len(stroke.Points) > 0 {
				all = append(all, *stroke)
			}
		}
	}
	return all, nil
}

// parseV6LineBlock extracts a Stroke from a SceneLineItemBlock's content.
//
// Layout:
//
//	Tags 1–4 (ID):  parent_id, item_id, left_id, right_id
//	Tag  5   (Byte4): deleted_length
//	Tag  6   (Length4): line value subblock
//	  byte 0:        item_type (0x03)
//	  Tag 1 (Byte4): tool_id
//	  Tag 2 (Byte4): color_id
//	  Tag 3 (Byte8): thickness_scale
//	  Tag 4 (Byte4): starting_length
//	  Tag 5 (Length4): raw points (14 bytes each)
func parseV6LineBlock(data []byte) (*Stroke, error) {
	// Skip tagged fields with field indices 1–5.
	pos := skipTaggedFields(data, 0, 5)

	// Field 6 must be a Length4 subblock (tag byte = 0x6C).
	if pos >= len(data) || data[pos] != byte((6<<4)|tagLength4) {
		return nil, nil
	}
	pos++

	if pos+4 > len(data) {
		return nil, nil
	}
	subLen := int(binary.LittleEndian.Uint32(data[pos:]))
	pos += 4

	if pos+subLen > len(data) {
		return nil, nil
	}
	return parseV6LineSubblock(data[pos : pos+subLen])
}

// parseV6LineSubblock decodes the line value subblock.
func parseV6LineSubblock(data []byte) (*Stroke, error) {
	if len(data) == 0 {
		return nil, nil
	}

	// First byte: item_type (0x03 for SceneLineItem). Skip it.
	pos := 1

	var toolID, colorID uint32
	var points []Point

	for pos < len(data) {
		tagByte := data[pos]
		fieldIdx := int(tagByte >> 4)
		tagType := int(tagByte & 0x0F)
		pos++

		switch {
		case fieldIdx == 1 && tagType == tagByte4: // tool_id
			if pos+4 > len(data) {
				return nil, nil
			}
			toolID = binary.LittleEndian.Uint32(data[pos:])
			pos += 4

		case fieldIdx == 2 && tagType == tagByte4: // color_id
			if pos+4 > len(data) {
				return nil, nil
			}
			colorID = binary.LittleEndian.Uint32(data[pos:])
			pos += 4

		case fieldIdx == 3 && tagType == tagByte8: // thickness_scale (float64)
			pos += 8

		case fieldIdx == 4 && tagType == tagByte4: // starting_length (float32)
			pos += 4

		case fieldIdx == 5 && tagType == tagLength4: // points subblock
			if pos+4 > len(data) {
				return nil, nil
			}
			ptsLen := int(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
			if pos+ptsLen > len(data) {
				return nil, nil
			}
			pts := parseV6Points(data[pos : pos+ptsLen])
			pos += ptsLen
			points = pts

		default:
			// Skip field we don't recognise.
			pos = skipOneTaggedValue(data, pos, tagType)
		}
	}

	if len(points) == 0 {
		return nil, nil
	}
	return &Stroke{
		Color:  int(colorID),
		Tool:   int(toolID),
		Width:  2.0, // v6 base width; modulated per-point by Width field
		Points: points,
	}, nil
}

// parseV6Points converts raw point bytes (14 bytes per point) into Points.
//
// Per-point layout:
//
//	0–3:   x        float32 LE
//	4–7:   y        float32 LE
//	8–9:   speed    uint16 LE  (ignored)
//	10–11: width    uint16 LE
//	12:    direction uint8     (ignored)
//	13:    pressure uint8
func parseV6Points(data []byte) []Point {
	const stride = 14
	n := len(data) / stride
	pts := make([]Point, n)
	for i := 0; i < n; i++ {
		off := i * stride
		pts[i] = Point{
			X:        math.Float32frombits(binary.LittleEndian.Uint32(data[off:])),
			Y:        math.Float32frombits(binary.LittleEndian.Uint32(data[off+4:])),
			Width:    float32(binary.LittleEndian.Uint16(data[off+10:])),
			Pressure: float32(data[off+13]) / 255.0,
		}
	}
	return pts
}

// ─────────────────────────────────── tag helpers ──────────────────────────────

// skipTaggedFields advances pos past all consecutive tagged fields whose
// field index is in [1, maxFieldIdx].
func skipTaggedFields(data []byte, pos, maxFieldIdx int) int {
	for pos < len(data) {
		tagByte := data[pos]
		fieldIdx := int(tagByte >> 4)
		tagType := int(tagByte & 0x0F)

		if fieldIdx == 0 || fieldIdx > maxFieldIdx {
			break
		}
		pos++ // consume tag byte
		pos = skipOneTaggedValue(data, pos, tagType)
	}
	return pos
}

// skipOneTaggedValue advances pos past the value for a field of the given tagType.
func skipOneTaggedValue(data []byte, pos, tagType int) int {
	switch tagType {
	case tagByte1:
		pos++
	case tagByte4:
		pos += 4
	case tagByte8:
		pos += 8
	case tagLength4:
		if pos+4 <= len(data) {
			l := int(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4 + l
		}
	case tagID: // CrdtId: 1 byte part1 + varint part2
		pos++ // part1
		for pos < len(data) {
			b := data[pos]
			pos++
			if b&0x80 == 0 {
				break
			}
		}
	}
	return pos
}
