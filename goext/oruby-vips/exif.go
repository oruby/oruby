package vips

import (
	"bytes"
	"encoding/binary"
)

// readExifOrientation inspects JPEG APP1 EXIF or TIFF bytes and returns the
// EXIF IFD0 Orientation tag value (1..8), or 0 if not present.
func readExifOrientation(data []byte) int {
	if len(data) < 12 {
		return 0
	}
	// Direct TIFF header (II*\0 or MM\0*)
	if (data[0] == 'I' && data[1] == 'I') || (data[0] == 'M' && data[1] == 'M') {
		return parseTIFFOrientation(data)
	}
	// JPEG SOI: FF D8
	if data[0] != 0xFF || data[1] != 0xD8 {
		return 0
	}
	pos := 2
	for pos+4 <= len(data) {
		if data[pos] != 0xFF {
			break
		}
		for pos < len(data) && data[pos] == 0xFF {
			pos++
		}
		if pos >= len(data) {
			break
		}
		marker := data[pos]
		pos++
		if marker == 0xD9 || marker == 0xDA { // EOI or SOS
			break
		}
		if pos+2 > len(data) {
			break
		}
		segLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if segLen < 2 || pos+segLen > len(data) {
			break
		}
		seg := data[pos+2 : pos+segLen]
		pos += segLen
		// APP1 (0xE1) starting with "Exif\0\0"
		if marker == 0xE1 && len(seg) > 6 && bytes.Equal(seg[:6], []byte("Exif\x00\x00")) {
			if ori := parseTIFFOrientation(seg[6:]); ori >= 1 && ori <= 8 {
				return ori
			}
		}
	}
	return 0
}

func parseTIFFOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	if tiff[0] == 'I' && tiff[1] == 'I' {
		bo = binary.LittleEndian
	} else if tiff[0] == 'M' && tiff[1] == 'M' {
		bo = binary.BigEndian
	} else {
		return 0
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return 0
	}
	ifdOff := int(bo.Uint32(tiff[4:8]))
	if ifdOff < 0 || ifdOff+2 > len(tiff) {
		return 0
	}
	numEntries := int(bo.Uint16(tiff[ifdOff : ifdOff+2]))
	off := ifdOff + 2
	for i := 0; i < numEntries && off+12 <= len(tiff); i++ {
		tag := bo.Uint16(tiff[off : off+2])
		if tag == 0x0112 { // Orientation
			val := int(bo.Uint16(tiff[off+8 : off+10]))
			if val >= 1 && val <= 8 {
				return val
			}
			return 0
		}
		off += 12
	}
	return 0
}

// exifOrientationString formats orientation 1..8 in the exact libvips format
// matched by Rails ActiveStorage::Analyzer::ImageAnalyzer::Vips::ROTATIONS.
func exifOrientationString(ori int) string {
	names := map[int]string{
		1: "1 (Top-left, Short, 1 components, 2 bytes)",
		2: "2 (Top-right, Short, 1 components, 2 bytes)",
		3: "3 (Bottom-right, Short, 1 components, 2 bytes)",
		4: "4 (Bottom-left, Short, 1 components, 2 bytes)",
		5: "5 (Left-top, Short, 1 components, 2 bytes)",
		6: "6 (Right-top, Short, 1 components, 2 bytes)",
		7: "7 (Right-bottom, Short, 1 components, 2 bytes)",
		8: "8 (Left-bottom, Short, 1 components, 2 bytes)",
	}
	return names[ori]
}
