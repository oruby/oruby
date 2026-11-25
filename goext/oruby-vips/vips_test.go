package vips_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/ffi"
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/io/file"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/goext/oruby-vips"
)

// makeExifJPEG creates a valid 40x20 JPEG with an APP1 EXIF Orientation=6 (Right-top) segment.
func makeExifJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 6), uint8(y * 12), 100, 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	jb := raw.Bytes()
	// Build minimal little-endian TIFF with 1 IFD0 entry: tag 0x0112 (Orientation), type SHORT(3), count 1, value 6
	var tiff bytes.Buffer
	tiff.WriteString("II")
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(42))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(8)) // IFD0 offset
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(1)) // 1 entry
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(3)) // SHORT
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(1)) // count
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(6)) // Right-top
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0)) // padding
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(0)) // next IFD

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var app1 bytes.Buffer
	app1.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&app1, binary.BigEndian, uint16(len(payload)+2))
	app1.Write(payload)

	// Splice APP1 right after SOI (FF D8)
	out := make([]byte, 0, len(jb)+app1.Len())
	out = append(out, jb[:2]...)
	out = append(out, app1.Bytes()...)
	out = append(out, jb[2:]...)
	return out
}

func TestPureGoVipsAndExifAndImageProcessing(t *testing.T) {
	t.Setenv("ORUBY_VIPS_BACKEND", "go")
	tmpDir := t.TempDir()
	exifPath := filepath.Join(tmpDir, "rotated.jpg")
	if err := os.WriteFile(exifPath, makeExifJPEG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	extlessPath := filepath.Join(tmpDir, "ActiveStorage-blob-no-ext")
	if err := os.WriteFile(extlessPath, makeExifJPEG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(tmpDir, "out.png")

	mrb := oruby.MrbOpen()
	defer mrb.Close()
	mrb.SetGV("$EXIF_PATH", mrb.StrNew(exifPath))
	mrb.SetGV("$EXTLESS_PATH", mrb.StrNew(extlessPath))
	mrb.SetGV("$OUT_PATH", mrb.StrNew(outPath))

	_, err := mrb.Eval(`
require "ruby-vips"
require "oruby-vips"
require "vips"
require "image_processing/vips"

raise "expected :go backend" unless Vips.oruby_backend == :go

# Create, resize, crop, rotate, flip, blur, sharpen, grayscale, composite
img = Vips::Image.black(100, 60, bands: 3)
raise "dims" unless img.width == 100 && img.height == 60 && img.bands == 3
raise "avg" unless img.avg == 0.0

thumb = img.thumbnail_image(50)
raise "thumb #{thumb.size.inspect}" unless thumb.width == 50 && thumb.height == 30

filled = img.thumbnail_image(40, height: 40, crop: :centre)
raise "fill #{filled.size.inspect}" unless filled.width == 40 && filled.height == 40

cropped = img.crop(10, 10, 20, 15)
raise "crop #{cropped.size.inspect}" unless cropped.width == 20 && cropped.height == 15

rot = cropped.rot90
raise "rot90 #{rot.size.inspect}" unless rot.width == 15 && rot.height == 20

padded = cropped.add_alpha.gravity("centre", 40, 40, background: [255, 0, 0, 255])
raise "gravity" unless padded.width == 40 && padded.height == 40 && padded.avg > 0

mask = Vips::Image.new_from_array([[-1, -1, -1], [-1, 32, -1], [-1, -1, -1]], 24)
sharp = padded.conv(mask)
raise "conv" unless sharp.size == [40, 40]

blurred = padded.gaussblur(1.5)
raise "blur" unless blurred.size == [40, 40]

gray = padded.colourspace(:b_w)
raise "gray" unless gray.bands == 1

# Encode/decode roundtrips: PNG, JPEG, GIF, BMP, TIFF
[".png", ".jpg", ".gif", ".bmp", ".tiff"].each do |ext|
  buf = padded.write_to_buffer(ext, Q: 80)
  re = Vips::Image.new_from_buffer(buf, "")
  raise "roundtrip #{ext}" unless re.size == [40, 40]
end

# Unsupported output formats raise Vips::Error cleanly
raised = false
begin
  padded.write_to_buffer(".avif")
rescue Vips::Error => e
  raised = e.message.include?("libvips")
end
raise "expected Vips::Error on .avif" unless raised

# EXIF orientation detection & autorot (40x20 with Orientation=6 -> 20x40)
ex = Vips::Image.new_from_file($EXIF_PATH)
raise "exif raw size" unless ex.size == [40, 20]
ori = ex.get("exif-ifd0-Orientation")
raise "exif orientation=#{ori.inspect}" unless ori.include?("Right-top")
upright = ex.autorot
raise "autorot size=#{upright.size.inspect}" unless upright.size == [20, 40]

# Built-in ImageProcessing::Vips fallback
raise "valid_image?" unless ImageProcessing::Vips.valid_image?($EXIF_PATH)
ImageProcessing::Vips.source($EXIF_PATH).resize_to_fill(16, 16).convert("png").call(destination: $OUT_PATH)
saved = Vips::Image.new_from_file($OUT_PATH)
raise "pipeline out=#{saved.size.inspect}" unless saved.size == [16, 16]

# Magic-byte sniffing on extensionless ActiveStorage Tempfile
raise "magic sniff" unless Vips.vips_foreign_find_load($EXTLESS_PATH) == "VipsForeignLoadJpeg"

# Raw memory roundtrip, getpoint, invert, linear, extract_band
raw = "\x0A\x14\x1E".b * 4
mem = Vips::Image.new_from_memory(raw, 2, 2, 3, :uchar)
raise "mem roundtrip" unless mem.write_to_memory == raw
raise "getpoint" unless mem.getpoint(0, 0) == [10.0, 20.0, 30.0]
raise "invert" unless mem.invert.getpoint(0, 0) == [245.0, 235.0, 225.0]
raise "linear" unless mem.linear(2.0, 5.0).getpoint(0, 0) == [25.0, 45.0, 65.0]
raise "extract_band" unless mem.extract_band(1).bands == 1 && mem.extract_band(1).getpoint(0, 0) == [20.0]
`)
	if err != nil {
		t.Fatalf("pure-Go vips test failed: %v", err)
	}
}
