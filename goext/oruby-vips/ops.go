package vips

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	"golang.org/x/image/tiff"
	_ "golang.org/x/image/webp" // registers WebP decoder
)

const maxCoord = 10_000_000

type goImage struct {
	img         *image.NRGBA
	bands       int
	hasAlpha    bool
	loader      string
	orientation int // EXIF 1..8 (0 = unset)
	meta        map[string]any
	// Optional float convolution kernel (from Vips::Image.new_from_array)
	kernel      [][]float64
	kernelScale float64
	kernelOff   float64
}

func toNRGBA(src image.Image) (*image.NRGBA, int, bool) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Copy(dst, image.Point{}, src, b, draw.Src, nil)

	hasAlpha := false
	switch src.(type) {
	case *image.NRGBA, *image.RGBA, *image.NRGBA64, *image.RGBA64, *image.Paletted:
		// Check whether any pixel has alpha < 255 or format natively carries alpha
		hasAlpha = true
	case *image.Gray, *image.Gray16:
		return dst, 1, false
	}
	bands := 3
	if hasAlpha {
		bands = 4
	}
	return dst, bands, hasAlpha
}

func sniffLoaderMagic(data []byte) string {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}) {
		return "VipsForeignLoadPng"
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "VipsForeignLoadJpeg"
	}
	if len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))) {
		return "VipsForeignLoadGif"
	}
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return "VipsForeignLoadWebp"
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		return "VipsForeignLoadBmp"
	}
	if len(data) >= 4 && ((data[0] == 'I' && data[1] == 'I' && data[2] == 42 && data[3] == 0) ||
		(data[0] == 'M' && data[1] == 'M' && data[2] == 0 && data[3] == 42)) {
		return "VipsForeignLoadTiff"
	}
	return ""
}

func sniffFileLoader(path string) string {
	clean, _ := splitVipsFilename(path)
	if f, err := os.Open(clean); err == nil {
		var hdr [16]byte
		n, _ := f.Read(hdr[:])
		_ = f.Close()
		if l := sniffLoaderMagic(hdr[:n]); l != "" {
			return l
		}
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(clean)), ".")
	return loaderNameForFormat(ext)
}

func loaderNameForFormat(format string) string {
	switch strings.ToLower(format) {
	case "png":
		return "VipsForeignLoadPng"
	case "jpeg", "jpg":
		return "VipsForeignLoadJpeg"
	case "gif":
		return "VipsForeignLoadGif"
	case "webp":
		return "VipsForeignLoadWebp"
	case "bmp":
		return "VipsForeignLoadBmp"
	case "tiff", "tif":
		return "VipsForeignLoadTiff"
	default:
		return ""
	}
}

func saverNameForExt(ext string) string {
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if idx := strings.IndexByte(ext, '['); idx >= 0 {
		ext = ext[:idx]
	}
	switch ext {
	case "png":
		return "VipsForeignSavePng"
	case "jpg", "jpeg", "jpe":
		return "VipsForeignSaveJpeg"
	case "gif":
		return "VipsForeignSaveGif"
	case "bmp":
		return "VipsForeignSaveBmp"
	case "tif", "tiff":
		return "VipsForeignSaveTiff"
	default:
		return ""
	}
}

func splitVipsFilename(raw string) (filename, optStr string) {
	if idx := strings.IndexByte(raw, '['); idx >= 0 && strings.HasSuffix(raw, "]") {
		return raw[:idx], raw[idx:]
	}
	return raw, ""
}

func parseVipsOptionString(optStr string) map[string]string {
	res := map[string]string{}
	s := strings.TrimSpace(optStr)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		s = s[1 : len(s)-1]
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if eq := strings.IndexByte(part, '='); eq >= 0 {
			res[strings.TrimSpace(part[:eq])] = strings.TrimSpace(part[eq+1:])
		} else {
			res[part] = "true"
		}
	}
	return res
}

func decodeGoImage(data []byte, hintPath string) (*goImage, error) {
	ori := readExifOrientation(data)
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		ext := strings.ToLower(filepath.Ext(hintPath))
		if ext == ".avif" || ext == ".heic" || ext == ".heif" || ext == ".svg" || ext == ".pdf" {
			return nil, fmt.Errorf("unsupported format %s in pure-Go Vips backend (install libvips for full %s support)", ext, strings.TrimPrefix(ext, "."))
		}
		return nil, fmt.Errorf("unable to load image: %w", err)
	}
	nrgba, bands, hasAlpha := toNRGBA(img)
	// PNG with opaque RGB vs RGBA
	if format == "jpeg" {
		bands = 3
		hasAlpha = false
	}
	meta := map[string]any{
		"vips-loader": loaderNameForFormat(format),
	}
	if ori >= 1 && ori <= 8 {
		meta["orientation"] = ori
		meta["exif-ifd0-Orientation"] = exifOrientationString(ori)
	}
	return &goImage{
		img:         nrgba,
		bands:       bands,
		hasAlpha:    hasAlpha,
		loader:      loaderNameForFormat(format),
		orientation: ori,
		meta:        meta,
	}, nil
}

func loadGoImageFile(path string) (*goImage, error) {
	cleanPath, _ := splitVipsFilename(path)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}
	return decodeGoImage(data, cleanPath)
}

func encodeGoImage(g *goImage, ext string, quality int) ([]byte, error) {
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if idx := strings.IndexByte(ext, '['); idx >= 0 {
		optMap := parseVipsOptionString(ext[idx:])
		if qStr, ok := optMap["Q"]; ok {
			if q, err := strconv.Atoi(qStr); err == nil {
				quality = q
			}
		}
		ext = ext[:idx]
	}
	if quality <= 0 {
		quality = 85
	}
	if quality > 100 {
		quality = 100
	}
	var buf bytes.Buffer
	switch ext {
	case "png":
		if err := png.Encode(&buf, g.img); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "jpg", "jpeg", "jpe":
		if err := jpeg.Encode(&buf, g.img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "gif":
		if err := gif.Encode(&buf, g.img, nil); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "bmp":
		if err := bmp.Encode(&buf, g.img); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "tif", "tiff":
		if err := tiff.Encode(&buf, g.img, nil); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported output format .%s in pure-Go Vips backend (install libvips for .%s encoding)", ext, ext)
	}
}

func (g *goImage) width() int {
	if g.img == nil {
		return len(g.kernel)
	}
	return g.img.Bounds().Dx()
}

func (g *goImage) height() int {
	if g.img == nil {
		if len(g.kernel) > 0 {
			return len(g.kernel[0])
		}
		return 0
	}
	return g.img.Bounds().Dy()
}

func (g *goImage) cloneWithImg(dst *image.NRGBA) *goImage {
	cp := &goImage{
		img:         dst,
		bands:       g.bands,
		hasAlpha:    g.hasAlpha,
		loader:      g.loader,
		orientation: g.orientation,
		meta:        make(map[string]any, len(g.meta)),
	}
	for k, v := range g.meta {
		cp.meta[k] = v
	}
	return cp
}

func newBlackImage(w, h, bands int) *goImage {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if bands < 1 {
		bands = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := dst.PixOffset(x, y)
			dst.Pix[off+3] = 255
		}
	}
	return &goImage{
		img:      dst,
		bands:    bands,
		hasAlpha: bands == 4 || bands == 2,
		meta:     map[string]any{},
	}
}

func (g *goImage) avg() float64 {
	w, h := g.width(), g.height()
	if w == 0 || h == 0 || g.img == nil {
		return 0
	}
	var sum uint64
	pix := g.img.Pix
	stride := g.img.Stride
	for y := 0; y < h; y++ {
		row := pix[y*stride:]
		for x := 0; x < w; x++ {
			off := x * 4
			sum += uint64(row[off]) + uint64(row[off+1]) + uint64(row[off+2])
		}
	}
	return float64(sum) / float64(w*h*3)
}

func (g *goImage) resizeTo(dstW, dstH int, kernel string) *goImage {
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	if dstW == g.width() && dstH == g.height() {
		dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
		copy(dst.Pix, g.img.Pix)
		return g.cloneWithImg(dst)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	var scaler draw.Scaler = draw.CatmullRom
	switch strings.ToLower(kernel) {
	case "nearest":
		scaler = draw.NearestNeighbor
	case "linear":
		scaler = draw.BiLinear
	}
	scaler.Scale(dst, dst.Bounds(), g.img, g.img.Bounds(), draw.Over, nil)
	return g.cloneWithImg(dst)
}

func (g *goImage) cropRect(left, top, w, h int) (*goImage, error) {
	sw, sh := g.width(), g.height()
	if left < 0 || top < 0 || w <= 0 || h <= 0 || left+w > sw || top+h > sh {
		return nil, fmt.Errorf("crop area (%d,%d %dx%d) out of bounds for %dx%d image", left, top, w, h, sw, sh)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Copy(dst, image.Point{}, g.img, image.Rect(left, top, left+w, top+h), draw.Src, nil)
	return g.cloneWithImg(dst), nil
}

// thumbnail implements Vips::Image#thumbnail_image / Vips::Image.thumbnail semantics:
// size: "both" (default), "down" (no upscale), "up" (no downscale), "force"
// crop: "none" (default), "centre"/"center"/"attention"/"entropy"/"high"/"low"
func (g *goImage) thumbnail(targetW, targetH int, sizeMode, cropMode string) (*goImage, error) {
	sw, sh := g.width(), g.height()
	if sw == 0 || sh == 0 {
		return nil, fmt.Errorf("empty image")
	}
	if targetW <= 0 && targetH <= 0 {
		return nil, fmt.Errorf("width or height required")
	}
	if targetW <= 0 {
		targetW = maxCoord
	}
	if targetH <= 0 {
		targetH = maxCoord
	}

	sizeMode = strings.ToLower(sizeMode)
	cropMode = strings.ToLower(cropMode)
	cropping := cropMode != "" && cropMode != "none"

	if sizeMode == "force" {
		w := targetW
		h := targetH
		if w >= maxCoord {
			w = sw
		}
		if h >= maxCoord {
			h = sh
		}
		return g.resizeTo(w, h, "lanczos3"), nil
	}

	scaleW := float64(targetW) / float64(sw)
	scaleH := float64(targetH) / float64(sh)

	var scale float64
	if cropping {
		// Fill target box, then crop excess
		scale = math.Max(scaleW, scaleH)
	} else {
		// Fit inside target box preserving aspect ratio
		scale = math.Min(scaleW, scaleH)
	}

	if sizeMode == "down" && scale > 1.0 {
		scale = 1.0
	} else if sizeMode == "up" && scale < 1.0 {
		scale = 1.0
	}

	newW := int(math.Round(float64(sw) * scale))
	newH := int(math.Round(float64(sh) * scale))
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	resized := g.resizeTo(newW, newH, "lanczos3")
	if !cropping {
		return resized, nil
	}
	cw := targetW
	ch := targetH
	if cw > resized.width() {
		cw = resized.width()
	}
	if ch > resized.height() {
		ch = resized.height()
	}
	left := (resized.width() - cw) / 2
	top := (resized.height() - ch) / 2
	switch cropMode {
	case "low":
		left, top = 0, 0
	case "high":
		left, top = resized.width()-cw, resized.height()-ch
	}
	return resized.cropRect(left, top, cw, ch)
}

func (g *goImage) rot90() *goImage {
	sw, sh := g.width(), g.height()
	dst := image.NewNRGBA(image.Rect(0, 0, sh, sw))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			so := g.img.PixOffset(x, y)
			do := dst.PixOffset(sh-1-y, x)
			copy(dst.Pix[do:do+4], g.img.Pix[so:so+4])
		}
	}
	return g.cloneWithImg(dst)
}

func (g *goImage) rot180() *goImage {
	sw, sh := g.width(), g.height()
	dst := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			so := g.img.PixOffset(x, y)
			do := dst.PixOffset(sw-1-x, sh-1-y)
			copy(dst.Pix[do:do+4], g.img.Pix[so:so+4])
		}
	}
	return g.cloneWithImg(dst)
}

func (g *goImage) rot270() *goImage {
	sw, sh := g.width(), g.height()
	dst := image.NewNRGBA(image.Rect(0, 0, sh, sw))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			so := g.img.PixOffset(x, y)
			do := dst.PixOffset(y, sw-1-x)
			copy(dst.Pix[do:do+4], g.img.Pix[so:so+4])
		}
	}
	return g.cloneWithImg(dst)
}

func (g *goImage) flip(horizontal bool) *goImage {
	sw, sh := g.width(), g.height()
	dst := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			so := g.img.PixOffset(x, y)
			dx, dy := x, y
			if horizontal {
				dx = sw - 1 - x
			} else {
				dy = sh - 1 - y
			}
			do := dst.PixOffset(dx, dy)
			copy(dst.Pix[do:do+4], g.img.Pix[so:so+4])
		}
	}
	return g.cloneWithImg(dst)
}

func (g *goImage) autorot() *goImage {
	var out *goImage
	switch g.orientation {
	case 2:
		out = g.flip(true)
	case 3:
		out = g.rot180()
	case 4:
		out = g.flip(false)
	case 5:
		out = g.rot90().flip(true)
	case 6:
		out = g.rot90()
	case 7:
		out = g.rot270().flip(true)
	case 8:
		out = g.rot270()
	default:
		return g
	}
	out.orientation = 1
	out.meta["orientation"] = 1
	delete(out.meta, "exif-ifd0-Orientation")
	return out
}

func (g *goImage) rotateAngle(deg float64, bg color.NRGBA) *goImage {
	norm := math.Mod(deg, 360)
	if norm < 0 {
		norm += 360
	}
	switch norm {
	case 0:
		return g
	case 90:
		return g.rot90()
	case 180:
		return g.rot180()
	case 270:
		return g.rot270()
	}
	rad := norm * math.Pi / 180.0
	sin, cos := math.Sin(rad), math.Cos(rad)
	sw, sh := float64(g.width()), float64(g.height())
	newW := int(math.Ceil(math.Abs(sw*cos) + math.Abs(sh*sin)))
	newH := int(math.Ceil(math.Abs(sw*sin) + math.Abs(sh*cos)))
	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	cx, cy := sw/2.0, sh/2.0
	ncx, ncy := float64(newW)/2.0, float64(newH)/2.0
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			dx := float64(x) - ncx
			dy := float64(y) - ncy
			sx := int(math.Round(dx*cos + dy*sin + cx))
			sy := int(math.Round(-dx*sin + dy*cos + cy))
			if sx >= 0 && sx < g.width() && sy >= 0 && sy < g.height() {
				so := g.img.PixOffset(sx, sy)
				do := dst.PixOffset(x, y)
				copy(dst.Pix[do:do+4], g.img.Pix[so:so+4])
			} else {
				dst.SetNRGBA(x, y, bg)
			}
		}
	}
	return g.cloneWithImg(dst)
}

func (g *goImage) gravityPad(dir string, targetW, targetH int, bg color.NRGBA) *goImage {
	if targetW < 1 {
		targetW = g.width()
	}
	if targetH < 1 {
		targetH = g.height()
	}
	dst := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
	// Fill background
	for y := 0; y < targetH; y++ {
		for x := 0; x < targetW; x++ {
			dst.SetNRGBA(x, y, bg)
		}
	}
	sw, sh := g.width(), g.height()
	ox := (targetW - sw) / 2
	oy := (targetH - sh) / 2
	d := strings.ToLower(strings.ReplaceAll(dir, "_", "-"))
	switch d {
	case "north":
		oy = 0
	case "south":
		oy = targetH - sh
	case "west":
		ox = 0
	case "east":
		ox = targetW - sw
	case "north-west":
		ox, oy = 0, 0
	case "north-east":
		ox, oy = targetW-sw, 0
	case "south-west":
		ox, oy = 0, targetH-sh
	case "south-east":
		ox, oy = targetW-sw, targetH-sh
	}
	draw.Draw(dst, image.Rect(ox, oy, ox+sw, oy+sh), g.img, image.Point{}, draw.Over)
	out := g.cloneWithImg(dst)
	if bg.A < 255 {
		out.hasAlpha = true
		out.bands = 4
	}
	return out
}

func (g *goImage) compositeOver(overlay *goImage) *goImage {
	w, h := g.width(), g.height()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(dst.Pix, g.img.Pix)
	draw.Draw(dst, dst.Bounds(), overlay.img, image.Point{}, draw.Over)
	return g.cloneWithImg(dst)
}

func (g *goImage) toGrayscale() *goImage {
	w, h := g.width(), g.height()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			so := g.img.PixOffset(x, y)
			r, gr, b, a := float64(g.img.Pix[so]), float64(g.img.Pix[so+1]), float64(g.img.Pix[so+2]), g.img.Pix[so+3]
			luma := uint8(math.Round(0.299*r + 0.587*gr + 0.114*b))
			do := dst.PixOffset(x, y)
			dst.Pix[do] = luma
			dst.Pix[do+1] = luma
			dst.Pix[do+2] = luma
			dst.Pix[do+3] = a
		}
	}
	out := g.cloneWithImg(dst)
	out.bands = 1
	return out
}

func (g *goImage) convolve(k [][]float64, scale, offset float64) *goImage {
	kh := len(k)
	if kh == 0 || len(k[0]) == 0 {
		return g
	}
	kw := len(k[0])
	if scale == 0 {
		scale = 1.0
	}
	w, h := g.width(), g.height()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	hy, hx := kh/2, kw/2
	clamp := func(v float64) uint8 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint8(math.Round(v))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sr, sg, sb float64
			for ky := 0; ky < kh; ky++ {
				sy := y + ky - hy
				if sy < 0 {
					sy = 0
				} else if sy >= h {
					sy = h - 1
				}
				for kx := 0; kx < kw; kx++ {
					sx := x + kx - hx
					if sx < 0 {
						sx = 0
					} else if sx >= w {
						sx = w - 1
					}
					wgt := k[ky][kx]
					so := g.img.PixOffset(sx, sy)
					sr += float64(g.img.Pix[so]) * wgt
					sg += float64(g.img.Pix[so+1]) * wgt
					sb += float64(g.img.Pix[so+2]) * wgt
				}
			}
			do := dst.PixOffset(x, y)
			dst.Pix[do] = clamp(sr/scale + offset)
			dst.Pix[do+1] = clamp(sg/scale + offset)
			dst.Pix[do+2] = clamp(sb/scale + offset)
			dst.Pix[do+3] = g.img.Pix[g.img.PixOffset(x, y)+3]
		}
	}
	return g.cloneWithImg(dst)
}

func (g *goImage) gaussBlur(sigma float64) *goImage {
	if sigma <= 0.1 {
		return g
	}
	radius := int(math.Ceil(sigma * 3))
	if radius > 25 {
		radius = 25
	}
	size := 2*radius + 1
	weights := make([]float64, size)
	sum := 0.0
	for i := -radius; i <= radius; i++ {
		w := math.Exp(-float64(i*i) / (2 * sigma * sigma))
		weights[i+radius] = w
		sum += w
	}
	for i := range weights {
		weights[i] /= sum
	}
	// Separable 1D horizontal + vertical
	hk := [][]float64{weights}
	vk := make([][]float64, size)
	for i, w := range weights {
		vk[i] = []float64{w}
	}
	return g.convolve(hk, 1.0, 0).convolve(vk, 1.0, 0)
}

func (g *goImage) writeToMemory() []byte {
	w, h, b := g.width(), g.height(), g.bands
	if b < 1 {
		b = 3
	}
	out := make([]byte, w*h*b)
	idx := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			so := g.img.PixOffset(x, y)
			for c := 0; c < b && c < 4; c++ {
				out[idx] = g.img.Pix[so+c]
				idx++
			}
		}
	}
	return out
}

func newFromMemory(data []byte, w, h, bands int) (*goImage, error) {
	if w <= 0 || h <= 0 || bands < 1 || bands > 4 {
		return nil, fmt.Errorf("invalid dimensions/bands (%dx%d, bands=%d)", w, h, bands)
	}
	need := w * h * bands
	if len(data) < need {
		return nil, fmt.Errorf("memory buffer too short: need %d bytes, got %d", need, len(data))
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	idx := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			do := dst.PixOffset(x, y)
			switch bands {
			case 1:
				v := data[idx]
				dst.Pix[do], dst.Pix[do+1], dst.Pix[do+2], dst.Pix[do+3] = v, v, v, 255
			case 2:
				v, a := data[idx], data[idx+1]
				dst.Pix[do], dst.Pix[do+1], dst.Pix[do+2], dst.Pix[do+3] = v, v, v, a
			case 3:
				dst.Pix[do], dst.Pix[do+1], dst.Pix[do+2], dst.Pix[do+3] = data[idx], data[idx+1], data[idx+2], 255
			case 4:
				copy(dst.Pix[do:do+4], data[idx:idx+4])
			}
			idx += bands
		}
	}
	return &goImage{img: dst, bands: bands, hasAlpha: bands == 2 || bands == 4, meta: map[string]any{}}, nil
}

func (g *goImage) getPoint(x, y int) ([]float64, error) {
	if x < 0 || y < 0 || x >= g.width() || y >= g.height() {
		return nil, fmt.Errorf("point (%d,%d) out of bounds", x, y)
	}
	off := g.img.PixOffset(x, y)
	b := g.bands
	if b < 1 || b > 4 {
		b = 4
	}
	res := make([]float64, b)
	for i := 0; i < b; i++ {
		res[i] = float64(g.img.Pix[off+i])
	}
	return res, nil
}

func (g *goImage) invert() *goImage {
	w, h := g.width(), g.height()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			so := g.img.PixOffset(x, y)
			do := dst.PixOffset(x, y)
			dst.Pix[do] = 255 - g.img.Pix[so]
			dst.Pix[do+1] = 255 - g.img.Pix[so+1]
			dst.Pix[do+2] = 255 - g.img.Pix[so+2]
			dst.Pix[do+3] = g.img.Pix[so+3]
		}
	}
	return g.cloneWithImg(dst)
}

func (g *goImage) linear(a, b float64) *goImage {
	w, h := g.width(), g.height()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			so := g.img.PixOffset(x, y)
			do := dst.PixOffset(x, y)
			dst.Pix[do] = clampU8(float64(g.img.Pix[so])*a + b)
			dst.Pix[do+1] = clampU8(float64(g.img.Pix[so+1])*a + b)
			dst.Pix[do+2] = clampU8(float64(g.img.Pix[so+2])*a + b)
			dst.Pix[do+3] = g.img.Pix[so+3]
		}
	}
	return g.cloneWithImg(dst)
}

func (g *goImage) extractBand(band int) (*goImage, error) {
	if band < 0 || band >= 4 {
		return nil, fmt.Errorf("band %d out of range", band)
	}
	w, h := g.width(), g.height()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			so := g.img.PixOffset(x, y)
			do := dst.PixOffset(x, y)
			v := g.img.Pix[so+band]
			dst.Pix[do], dst.Pix[do+1], dst.Pix[do+2], dst.Pix[do+3] = v, v, v, 255
		}
	}
	out := g.cloneWithImg(dst)
	out.bands = 1
	out.hasAlpha = false
	return out, nil
}
