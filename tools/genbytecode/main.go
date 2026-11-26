package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oruby/oruby"
)

func compileToMrb(mrb *oruby.MrbState, src []byte, filename string) ([]byte, error) {
	c := mrb.MrbCcontextNew()
	defer c.Free()
	c.Filename(filename)
	c.SetNoExec(true)
	c.SetCaptureErrors(true)

	p, err := mrb.ParseString(string(src), c)
	if err != nil {
		return nil, err
	}
	defer p.Free()

	if p.NErr() > 0 {
		var sb strings.Builder
		for i := 0; i < p.NErr(); i++ {
			e := p.ErrorBuffer(i)
			fmt.Fprintf(&sb, "%s:%d:%d: %s\n", filename, e.LineNo, e.Column, e.Message)
		}
		return nil, fmt.Errorf("parse error: %s", sb.String())
	}

	proc, err := mrb.GenerateCode(p)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if _, err := mrb.DumpIrep(proc.IRep(), oruby.MrbDumpDebugInfo, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildCoreExtOrb(mrb *oruby.MrbState) error {
	dir := filepath.Join("gem", "core_ext", "ruby")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".rb") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	builder := oruby.NewOrbBuilder(oruby.OrbHdrDebugInfo)
	manifest := make(map[string]string, len(names))

	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if len(data) == 0 {
			continue
		}
		mrbBytes, err := compileToMrb(mrb, data, "<internal:core_ext/"+name+">")
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := builder.AddEntry(name, oruby.OrbKindMrb, 0, 0644, mrbBytes); err != nil {
			return err
		}
		manifest[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := builder.AddEntry("_manifest.json", oruby.OrbKindAsset, 0, 0644, manifestJSON); err != nil {
		return err
	}
	if err := builder.WriteFile(filepath.Join("gem", "core_ext", "core_ext.orb")); err != nil {
		return err
	}
	return buildRbOrb(mrb)
}

func buildRbOrb(mrb *oruby.MrbState) error {
	patterns := []string{
		"gem/io/*.rb",
		"gem/io/popen/*.rb",
		"gem/regexp/*.rb",
		"gem/time/*.rb",
		"gem/env/*.rb",
		"gem/transcode/*.rb",
		"gem/signal/*.rb",
		"gem/process/*.rb",
		"gem/erubi/erb.rb",
		"gem/zlibgo/zlibgo.rb",
		"gem/stdlib/ruby/*.rb",
		"gem/uri/*.rb",
		"gem/socket/*.rb",
		"gem/date/*.rb",
		"gem/digest/*.rb",
		"gem/nethttp/*.rb",
		"gem/etc/*.rb",
		"gem/zlib/*.rb",
		"gem/ffi/*.rb",
		"gem/coverage/*.rb",
	}
	var paths []string
	for _, pat := range patterns {
		matches, err := filepath.Glob(pat)
		if err != nil {
			return err
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)

	builder := oruby.NewOrbBuilder(oruby.OrbHdrDebugInfo)
	seen := make(map[string]bool)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil || len(data) == 0 {
			continue
		}
		sum := fmt.Sprintf("%x", sha256.Sum256(data))
		if seen[sum] {
			continue
		}
		seen[sum] = true
		mrbBytes, err := compileToMrb(mrb, data, "<internal:"+filepath.Base(p)+">")
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if err := builder.AddEntry(sum, oruby.OrbKindMrb, 0, 0644, mrbBytes); err != nil {
			return err
		}
	}
	return builder.WriteFile(filepath.Join("gem", "rb", "precompiled.orb"))
}

type stdlibFileMeta struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type stdlibManifest struct {
	Prefix             string                    `json:"prefix"`
	Files              map[string]stdlibFileMeta `json:"files"`
	DefaultSpecsPrefix string                    `json:"default_specs_prefix,omitempty"`
	DefaultSpecs       map[string]stdlibFileMeta `json:"default_specs,omitempty"`
}

func buildStdlibOrb(mrb *oruby.MrbState, dir, outPath string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		realDir = absDir
	}

	var rels []string
	rgRb := filepath.Join(realDir, "rubygems.rb")
	if st, err := os.Stat(rgRb); err == nil && !st.IsDir() {
		rels = append(rels, "rubygems.rb")
	}
	rgDir := filepath.Join(realDir, "rubygems")
	if st, err := os.Stat(rgDir); err == nil && st.IsDir() {
		err = filepath.WalkDir(rgDir, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".rb") {
				rel, rerr := filepath.Rel(realDir, p)
				if rerr != nil {
					return rerr
				}
				rels = append(rels, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	sort.Strings(rels)

	builder := oruby.NewOrbBuilder(oruby.OrbHdrDebugInfo)
	manifest := stdlibManifest{
		Prefix: filepath.ToSlash(realDir),
		Files:  make(map[string]stdlibFileMeta, len(rels)),
	}

	for _, rel := range rels {
		fullPath := filepath.Join(realDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return err
		}
		mrbBytes, err := compileToMrb(mrb, data, fullPath)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if err := builder.AddEntry(rel, oruby.OrbKindMrb, 0, 0644, mrbBytes); err != nil {
			return err
		}
		manifest.Files[rel] = stdlibFileMeta{
			Size:   int64(len(data)),
			SHA256: fmt.Sprintf("%x", sha256.Sum256(data)),
		}
	}

	defaultSpecsDir := filepath.Join(filepath.Dir(realDir), "gems", filepath.Base(realDir), "specifications", "default")
	if st, err := os.Stat(defaultSpecsDir); err == nil && st.IsDir() {
		realSpecDir, err := filepath.EvalSymlinks(defaultSpecsDir)
		if err != nil {
			realSpecDir = defaultSpecsDir
		}
		specEntries, err := os.ReadDir(realSpecDir)
		if err != nil {
			return err
		}
		var specNames []string
		for _, se := range specEntries {
			if !se.IsDir() && strings.HasSuffix(se.Name(), ".gemspec") {
				specNames = append(specNames, se.Name())
			}
		}
		sort.Strings(specNames)
		manifest.DefaultSpecsPrefix = filepath.ToSlash(realSpecDir)
		manifest.DefaultSpecs = make(map[string]stdlibFileMeta, len(specNames))
		for _, name := range specNames {
			fullSpecPath := filepath.Join(realSpecDir, name)
			data, err := os.ReadFile(fullSpecPath)
			if err != nil {
				return err
			}
			mrbBytes, err := compileToMrb(mrb, data, fullSpecPath)
			if err != nil {
				return fmt.Errorf("default_specs/%s: %w", name, err)
			}
			if err := builder.AddEntry("default_specs/"+name, oruby.OrbKindMrb, 0, 0644, mrbBytes); err != nil {
				return err
			}
			manifest.DefaultSpecs[name] = stdlibFileMeta{
				Size:   int64(len(data)),
				SHA256: fmt.Sprintf("%x", sha256.Sum256(data)),
			}
		}
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := builder.AddEntry("_manifest.json", oruby.OrbKindAsset, 0, 0644, manifestJSON); err != nil {
		return err
	}
	return builder.WriteFile(outPath)
}

func buildStdlibExtraOrb(mrb *oruby.MrbState, dir, outPath string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		realDir = absDir
	}
	var rels []string
	err = filepath.WalkDir(realDir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rerr := filepath.Rel(realDir, p)
		if rerr != nil {
			return rerr
		}
		slash := filepath.ToSlash(rel)
		if d.IsDir() {
			if slash == "rubygems" || strings.Contains(slash, "-darwin") || strings.Contains(slash, "-linux") {
				return filepath.SkipDir
			}
			return nil
		}
		if slash == "rubygems.rb" || !strings.HasSuffix(slash, ".rb") {
			return nil
		}
		rels = append(rels, slash)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(rels)

	builder := oruby.NewOrbBuilder(oruby.OrbHdrDebugInfo)
	manifest := stdlibManifest{
		Prefix: filepath.ToSlash(realDir),
		Files:  make(map[string]stdlibFileMeta, len(rels)),
	}
	for _, rel := range rels {
		fullPath := filepath.Join(realDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		mrbBytes, err := compileToMrb(mrb, data, fullPath)
		if err != nil {
			log.Printf("warning: skipped %s: %v", rel, err)
			continue
		}
		if err := builder.AddEntry(rel, oruby.OrbKindMrb, 0, 0644, mrbBytes); err != nil {
			return err
		}
		manifest.Files[rel] = stdlibFileMeta{
			Size:   int64(len(data)),
			SHA256: fmt.Sprintf("%x", sha256.Sum256(data)),
		}
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := builder.AddEntry("_manifest.json", oruby.OrbKindAsset, 0, 0644, manifestJSON); err != nil {
		return err
	}
	return builder.WriteFile(outPath)
}

func main() {
	coreExt := flag.Bool("core-ext", false, "compile gem/core_ext/ruby/*.rb into gem/core_ext/core_ext.orb")
	stdlibDir := flag.String("stdlib-dir", "", "stdlib directory (e.g. lib/oruby/4.1.0)")
	stdlibExtra := flag.Bool("stdlib-extra", false, "compile non-rubygems stdlib .rb files into -out")
	outFile := flag.String("out", "", "output .orb file path for -stdlib-dir")
	flag.Parse()

	if !*coreExt && (*stdlibDir == "" || *outFile == "") {
		log.Fatal("usage: genbytecode -core-ext | [-stdlib-extra] -stdlib-dir DIR -out FILE")
	}

	mrb, err := oruby.NewCore()
	if err != nil {
		log.Fatalf("oruby.NewCore: %v", err)
	}
	defer mrb.Close()

	if *coreExt {
		if err := buildCoreExtOrb(mrb); err != nil {
			log.Fatalf("buildCoreExtOrb: %v", err)
		}
	}
	if *stdlibDir != "" {
		if *stdlibExtra {
			if err := buildStdlibExtraOrb(mrb, *stdlibDir, *outFile); err != nil {
				log.Fatalf("buildStdlibExtraOrb: %v", err)
			}
		} else if err := buildStdlibOrb(mrb, *stdlibDir, *outFile); err != nil {
			log.Fatalf("buildStdlibOrb: %v", err)
		}
	}
}
