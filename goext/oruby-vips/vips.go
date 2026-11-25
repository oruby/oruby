// Package vips provides a united Vips / ImageProcessing backend for ORuby:
//   - When libvips is installed on the host/container and the ruby-vips gem is
//     on $LOAD_PATH (unless ORUBY_VIPS_BACKEND=go), `require "vips"` loads the
//     unmodified ruby-vips gem via gem/ffi for full libvips capabilities
//     (AVIF, HEIC, lossy WebP, SVG, PDF, 300+ ops).
//   - When libvips is absent (lean containers) or ORUBY_VIPS_BACKEND=go,
//     `require "vips"` activates a built-in pure-Go Vips::Image backend using
//     Go stdlib image/* + golang.org/x/image (PNG, JPEG, GIF, BMP, TIFF,
//     WebP decode, EXIF auto-orient, resize/crop/pad/rotate/blur/sharpen/composite)
//     and raises Vips::Error cleanly on unsupported formats.
package vips

import (
	_ "embed"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed vips.rb
var vipsRb []byte

//go:embed image_processing_fallback.rb
var imageProcessingFallbackRb []byte

func init() {
	oruby.LazyGem("oruby-vips", initVips)
	oruby.GemAlias("vips", "oruby-vips")
	oruby.GemAlias("ruby-vips", "oruby-vips")
	oruby.LazyGem("image_processing", initImageProcessing)
	oruby.LazyGem("image_processing/vips", initImageProcessingVips)
}

func findFileOnLoadPath(mrb *oruby.MrbState, rel string) string {
	lp := mrb.GVGet(oruby.GvSym_LOAD_PATH)
	if !lp.IsArray() {
		return ""
	}
	for i := 0; i < oruby.RArrayLen(lp); i++ {
		dir := mrb.AryRef(lp, i).String()
		if dir == "" {
			continue
		}
		cand := filepath.Join(dir, rel)
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand
		}
	}
	return ""
}

func probeLibvipsAvailable(mrb *oruby.MrbState) bool {
	if _, err := mrb.Resolve("ffi"); err != nil {
		return false
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"libvips.42.dylib",
			"/opt/homebrew/lib/libvips.42.dylib",
			"/usr/local/lib/libvips.42.dylib",
			"/opt/local/lib/libvips.42.dylib",
		}
	case "windows":
		candidates = []string{"libvips-42.dll", "vips-42.dll"}
	default:
		candidates = []string{
			"libvips.so.42",
			"/usr/local/lib/libvips.so.42",
			"/usr/lib/libvips.so.42",
		}
	}
	dlCls := rb.ClassPath(mrb, "FFI::DynamicLibrary")
	if dlCls.IsNil() {
		return false
	}
	for _, path := range candidates {
		lib, err := mrb.Funcall(dlCls, mrb.Intern("open"), mrb.StrNew(path), mrb.FixnumValue(1))
		if err == nil && !lib.IsNil() {
			if mrb.RespondTo(lib, mrb.Intern("close")) {
				_, _ = mrb.Funcall(lib, mrb.Intern("close"))
			}
			return true
		}
		mrb.SetExc(nil)
	}
	return false
}

func initVips(mrb *oruby.MrbState) any {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("ORUBY_VIPS_BACKEND")))
	if mode == "" {
		mode = "auto"
	}

	if mode != "go" {
		vipsPath := findFileOnLoadPath(mrb, "vips.rb")
		hasLib := probeLibvipsAvailable(mrb)
		if hasLib && vipsPath != "" {
			if _, err := mrb.Funcall(mrb.KernelModule(), mrb.Intern("require"), mrb.StrNew(vipsPath)); err != nil {
				return err
			}
			vipsMod := mrb.ModuleGet("Vips")
			if !vipsMod.IsNil() {
				vipsMod.DefineClassMethod("oruby_backend", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
					return mrb.Intern("libvips").Value()
				}, mrb.ArgsNone())
			}
			return nil
		}
		if mode == "libvips" {
			return fmt.Errorf("ORUBY_VIPS_BACKEND=libvips requested, but libvips=%v, ruby-vips on $LOAD_PATH=%v", hasLib, vipsPath != "")
		}
	}

	initPureGoVips(mrb)
	return nil
}

func initImageProcessing(mrb *oruby.MrbState) any {
	if p := findFileOnLoadPath(mrb, "image_processing.rb"); p != "" {
		if _, err := mrb.Funcall(mrb.KernelModule(), mrb.Intern("require"), mrb.StrNew(p)); err != nil {
			return err
		}
		return nil
	}
	if _, err := mrb.Resolve("vips"); err != nil {
		return err
	}
	rb.LoadRuby(mrb, "<internal:image_processing>", imageProcessingFallbackRb)
	return nil
}

func initImageProcessingVips(mrb *oruby.MrbState) any {
	if p := findFileOnLoadPath(mrb, filepath.Join("image_processing", "vips.rb")); p != "" {
		if _, err := mrb.Funcall(mrb.KernelModule(), mrb.Intern("require"), mrb.StrNew(p)); err != nil {
			return err
		}
		return nil
	}
	if _, err := mrb.Resolve("image_processing"); err != nil {
		return err
	}
	return nil
}

func parseBGColor(mrb *oruby.MrbState, v oruby.Value, hasAlpha bool) color.NRGBA {
	if v.IsNil() {
		if hasAlpha {
			return color.NRGBA{0, 0, 0, 0}
		}
		return color.NRGBA{0, 0, 0, 255}
	}
	if v.IsInt() || v.IsFloat() {
		c := clampU8(v.Float64())
		return color.NRGBA{c, c, c, 255}
	}
	if v.IsArray() {
		n := oruby.RArrayLen(v)
		var vals [4]uint8
		vals[3] = 255
		for i := 0; i < n && i < 4; i++ {
			vals[i] = clampU8(mrb.AryRef(v, i).Float64())
		}
		if n == 1 {
			return color.NRGBA{vals[0], vals[0], vals[0], 255}
		}
		if n == 2 {
			return color.NRGBA{vals[0], vals[0], vals[0], vals[1]}
		}
		return color.NRGBA{vals[0], vals[1], vals[2], vals[3]}
	}
	return color.NRGBA{0, 0, 0, 255}
}

func clampU8(f float64) uint8 {
	if f < 0 {
		return 0
	}
	if f > 255 {
		return 255
	}
	return uint8(f)
}

func initPureGoVips(mrb *oruby.MrbState) {
	mod := mrb.DefineModule("Vips")
	errCls := mrb.DefineClassUnder(mod, "Error", mrb.ExcGet("StandardError"))
	imgCls := rb.DataClass(mrb.DefineClassUnder(mod, "Image", mrb.ObjectClass()))

	wrapImg := func(mrb *oruby.MrbState, g *goImage) oruby.Value {
		obj := mrb.ObjAlloc(oruby.MrbTTCData, imgCls)
		rb.Set(mrb, obj.Value(), g)
		return obj.Value()
	}

	getImg := func(mrb *oruby.MrbState, v oruby.Value) (*goImage, bool) {
		return rb.Get[*goImage](mrb, v)
	}

	// Module helpers used by image_processing/vips
	mod.DefineClassMethod("vips_filename_get_filename", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		fn, _ := splitVipsFilename(mrb.GetArgsFirst().String())
		return mrb.StrNew(fn)
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("vips_filename_get_options", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		_, opt := splitVipsFilename(mrb.GetArgsFirst().String())
		return mrb.StrNew(opt)
	}, mrb.ArgsReq(1))

	mod.DefineClassMethod("vips_foreign_find_load", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		if l := sniffFileLoader(mrb.GetArgsFirst().String()); l != "" {
			return mrb.StrNew(l)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	findSaveFn := func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		fn, _ := splitVipsFilename(mrb.GetArgsFirst().String())
		ext := filepath.Ext(fn)
		if ext == "" {
			ext = fn
		}
		if s := saverNameForExt(ext); s != "" {
			return mrb.StrNew(s)
		}
		return mrb.NilValue()
	}
	mod.DefineClassMethod("vips_foreign_find_save", findSaveFn, mrb.ArgsReq(1))
	mod.DefineClassMethod("vips_foreign_find_save_target", findSaveFn, mrb.ArgsReq(1))
	mod.DefineClassMethod("vips_foreign_find_save_buffer", findSaveFn, mrb.ArgsReq(1))

	// Vips::Image class constructors
	imgCls.DefineClassMethod("__new_from_file", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		path := mrb.GetArgsFirst().String()
		g, err := loadGoImageFile(path)
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return wrapImg(mrb, g)
	}, mrb.ArgsReq(1))

	imgCls.DefineClassMethod("__new_from_buffer", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs().Slice()
		data, err := rb.Bytes(mrb, args[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		g, err := decodeGoImage(data, "")
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return wrapImg(mrb, g)
	}, mrb.ArgsArg(1, 1))

	imgCls.DefineClassMethod("__black", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		return wrapImg(mrb, newBlackImage(a[0].Int(), a[1].Int(), a[2].Int()))
	}, mrb.ArgsReq(3))

	imgCls.DefineClassMethod("__new_from_memory", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		data, err := rb.Bytes(mrb, a[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		g, err := newFromMemory(data, a[1].Int(), a[2].Int(), a[3].Int())
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return wrapImg(mrb, g)
	}, mrb.ArgsReq(4))

	imgCls.DefineClassMethod("__new_from_array", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		arr := a[0]
		scale := a[1].Float64()
		off := a[2].Float64()
		if !arr.IsArray() {
			return mrb.Raise(errCls, "expected Array for new_from_array")
		}
		var mat [][]float64
		n := oruby.RArrayLen(arr)
		if n > 0 && mrb.AryRef(arr, 0).IsArray() {
			for i := 0; i < n; i++ {
				rowVal := mrb.AryRef(arr, i)
				rn := oruby.RArrayLen(rowVal)
				row := make([]float64, rn)
				for j := 0; j < rn; j++ {
					row[j] = mrb.AryRef(rowVal, j).Float64()
				}
				mat = append(mat, row)
			}
		} else {
			row := make([]float64, n)
			for i := 0; i < n; i++ {
				row[i] = mrb.AryRef(arr, i).Float64()
			}
			mat = [][]float64{row}
		}
		return wrapImg(mrb, &goImage{
			kernel:      mat,
			kernelScale: scale,
			kernelOff:   off,
			meta:        map[string]any{},
		})
	}, mrb.ArgsReq(3))

	// Instance inspection
	imgCls.DefineMethod("width", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return mrb.FixnumValue(g.width())
	}, mrb.ArgsNone())

	imgCls.DefineMethod("height", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return mrb.FixnumValue(g.height())
	}, mrb.ArgsNone())

	imgCls.DefineMethod("bands", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return mrb.FixnumValue(g.bands)
	}, mrb.ArgsNone())

	imgCls.DefineMethod("has_alpha?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return mrb.BoolValue(g.hasAlpha)
	}, mrb.ArgsNone())

	imgCls.DefineMethod("add_alpha", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		if g.hasAlpha {
			return self
		}
		cp := g.cloneWithImg(g.img)
		cp.hasAlpha = true
		if cp.bands == 3 {
			cp.bands = 4
		} else if cp.bands == 1 {
			cp.bands = 2
		}
		return wrapImg(mrb, cp)
	}, mrb.ArgsNone())

	imgCls.DefineMethod("avg", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return mrb.FloatValue(g.avg())
	}, mrb.ArgsNone())

	imgCls.DefineMethod("get", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		field := mrb.GetArgsFirst().String()
		switch field {
		case "width":
			return mrb.FixnumValue(g.width())
		case "height":
			return mrb.FixnumValue(g.height())
		case "bands":
			return mrb.FixnumValue(g.bands)
		}
		if v, ok := g.meta[field]; ok {
			switch vt := v.(type) {
			case string:
				return mrb.StrNew(vt)
			case int:
				return mrb.FixnumValue(vt)
			case float64:
				return mrb.FloatValue(vt)
			}
		}
		return mrb.Raise(errCls, fmt.Sprintf("unable to get %q: field not found", field))
	}, mrb.ArgsReq(1))

	imgCls.DefineMethod("get_fields", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		ary := mrb.AryNew()
		for _, k := range []string{"width", "height", "bands"} {
			mrb.AryPush(ary, mrb.StrNew(k))
		}
		for k := range g.meta {
			mrb.AryPush(ary, mrb.StrNew(k))
		}
		return ary
	}, mrb.ArgsNone())

	imgCls.DefineMethod("set", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		k := a[0].String()
		if a[1].IsInt() {
			g.meta[k] = a[1].Int()
		} else {
			g.meta[k] = mrb.String(a[1])
		}
		return self
	}, mrb.ArgsReq(2))

	imgCls.DefineMethod("remove", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		delete(g.meta, mrb.GetArgsFirst().String())
		return self
	}, mrb.ArgsReq(1))

	// Transformations
	imgCls.DefineMethod("__thumbnail_image", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		out, err := g.thumbnail(a[0].Int(), a[1].Int(), a[2].String(), a[3].String())
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return wrapImg(mrb, out)
	}, mrb.ArgsReq(4))

	imgCls.DefineMethod("__resize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		hscale := a[0].Float64()
		vscale := a[1].Float64()
		kernel := a[2].String()
		newW := int(float64(g.width())*hscale + 0.5)
		newH := int(float64(g.height())*vscale + 0.5)
		return wrapImg(mrb, g.resizeTo(newW, newH, kernel))
	}, mrb.ArgsReq(3))

	imgCls.DefineMethod("__crop", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		out, err := g.cropRect(a[0].Int(), a[1].Int(), a[2].Int(), a[3].Int())
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return wrapImg(mrb, out)
	}, mrb.ArgsReq(4))

	imgCls.DefineMethod("autorot", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return wrapImg(mrb, g.autorot())
	}, mrb.ArgsNone())

	imgCls.DefineMethod("rot90", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return wrapImg(mrb, g.rot90())
	}, mrb.ArgsNone())

	imgCls.DefineMethod("rot180", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return wrapImg(mrb, g.rot180())
	}, mrb.ArgsNone())

	imgCls.DefineMethod("rot270", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return wrapImg(mrb, g.rot270())
	}, mrb.ArgsNone())

	imgCls.DefineMethod("__rotate_angle", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		bg := parseBGColor(mrb, a[1], g.hasAlpha)
		return wrapImg(mrb, g.rotateAngle(a[0].Float64(), bg))
	}, mrb.ArgsReq(2))

	imgCls.DefineMethod("__flip", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		dir := strings.ToLower(mrb.GetArgsFirst().String())
		return wrapImg(mrb, g.flip(dir == "horizontal" || dir == "h"))
	}, mrb.ArgsReq(1))

	imgCls.DefineMethod("__gravity", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		bg := parseBGColor(mrb, a[3], g.hasAlpha)
		return wrapImg(mrb, g.gravityPad(a[0].String(), a[1].Int(), a[2].Int(), bg))
	}, mrb.ArgsReq(4))

	imgCls.DefineMethod("__composite_over", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		ov, ok := getImg(mrb, mrb.GetArgsFirst())
		if !ok {
			return mrb.Raise(errCls, "expected Vips::Image overlay")
		}
		return wrapImg(mrb, g.compositeOver(ov))
	}, mrb.ArgsReq(1))

	imgCls.DefineMethod("__colourspace", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		sp := strings.ToLower(mrb.GetArgsFirst().String())
		if sp == "b-w" || sp == "b_w" || sp == "grey16" || sp == "gray" {
			return wrapImg(mrb, g.toGrayscale())
		}
		return self
	}, mrb.ArgsReq(1))

	imgCls.DefineMethod("__gaussblur", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return wrapImg(mrb, g.gaussBlur(mrb.GetArgsFirst().Float64()))
	}, mrb.ArgsReq(1))

	imgCls.DefineMethod("__conv", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		mask, ok := getImg(mrb, mrb.GetArgsFirst())
		if !ok || len(mask.kernel) == 0 {
			return self
		}
		return wrapImg(mrb, g.convolve(mask.kernel, mask.kernelScale, mask.kernelOff))
	}, mrb.ArgsReq(1))

	imgCls.DefineMethod("__flatten", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		bg := parseBGColor(mrb, mrb.GetArgsFirst(), false)
		bg.A = 255
		out := g.gravityPad("centre", g.width(), g.height(), bg)
		out.hasAlpha = false
		out.bands = 3
		return wrapImg(mrb, out)
	}, mrb.ArgsReq(1))

	imgCls.DefineMethod("write_to_memory", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return mrb.Call(mrb.BytesValue(g.writeToMemory()), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsNone())

	imgCls.DefineMethod("getpoint", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		pts, err := g.getPoint(a[0].Int(), a[1].Int())
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		ary := mrb.AryNewCapa(len(pts))
		for _, v := range pts {
			mrb.AryPush(ary, mrb.FloatValue(v))
		}
		return ary
	}, mrb.ArgsReq(2))

	imgCls.DefineMethod("invert", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return wrapImg(mrb, g.invert())
	}, mrb.ArgsNone())

	imgCls.DefineMethod("copy", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		return wrapImg(mrb, g.cloneWithImg(g.img))
	}, mrb.ArgsAny())

	imgCls.DefineMethod("__linear", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		return wrapImg(mrb, g.linear(a[0].Float64(), a[1].Float64()))
	}, mrb.ArgsReq(2))

	imgCls.DefineMethod("__extract_band", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		out, err := g.extractBand(mrb.GetArgsFirst().Int())
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return wrapImg(mrb, out)
	}, mrb.ArgsReq(1))

	// Saving
	imgCls.DefineMethod("__write_to_file", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		cleanPath, optStr := splitVipsFilename(a[0].String())
		ext := filepath.Ext(cleanPath) + optStr
		data, err := encodeGoImage(g, ext, a[1].Int())
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		if err := os.WriteFile(cleanPath, data, 0o644); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(2))

	imgCls.DefineMethod("__write_to_target", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		filename := a[0].String()
		fmtStr := a[1].String()
		data, err := encodeGoImage(g, fmtStr, a[2].Int())
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		if err := os.WriteFile(filename, data, 0o644); err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(3))

	imgCls.DefineMethod("__write_to_buffer", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, _ := getImg(mrb, self)
		a := mrb.GetArgs().Slice()
		data, err := encodeGoImage(g, a[0].String(), a[1].Int())
		if err != nil {
			return mrb.Raise(errCls, err.Error())
		}
		return mrb.Call(mrb.BytesValue(data), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(2))

	rb.LoadRuby(mrb, "<internal:vips>", vipsRb)
}
