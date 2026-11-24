package oruby

// #include "go-orb.h"
import "C"
import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ORB (ORuby Bundle) format constants matching go-orb.h
const (
	OrbMagic        = "ORB\x00"
	OrbExt          = ".orb"
	OrbVersion      = uint16(C.ORB_VERSION)
	OrbHeaderSize   = int(C.ORB_HEADER_SIZE)
	OrbEntrySize    = int(C.ORB_ENTRY_SIZE)
	OrbAlignment    = int(C.ORB_ALIGNMENT)
	OrbNoEntrypoint = uint32(C.ORB_NO_ENTRYPOINT)
	OrbVirtualRoot  = "/@orb"
)

// OrbEntryKind identifies the payload type of an entry in an .orb bundle.
type OrbEntryKind uint8

const (
	OrbKindMrb   OrbEntryKind = C.ORB_KIND_MRB   // Precompiled mruby RiteVM bytecode (.mrb)
	OrbKindRuby  OrbEntryKind = C.ORB_KIND_RUBY  // Raw Ruby source (.rb)
	OrbKindAsset OrbEntryKind = C.ORB_KIND_ASSET // Static asset (.yml, .erb, .json, etc.)
	OrbKindDir   OrbEntryKind = C.ORB_KIND_DIR   // Virtual directory
)

// Entry and Header flag bitmasks
const (
	OrbFlagEntrypoint uint8 = C.ORB_FLAG_ENTRYPOINT
	OrbFlagLoadPath   uint8 = C.ORB_FLAG_LOAD_PATH
	OrbFlagExecutable uint8 = C.ORB_FLAG_EXECUTABLE

	OrbHdrDebugInfo uint16 = C.ORB_HDR_DEBUG_INFO
)

// OrbHeader represents the 32-byte fixed header of an .orb file.
type OrbHeader struct {
	Magic         [4]byte
	Version       uint16
	Flags         uint16
	EntryCount    uint32
	EntrypointIdx uint32
	IndexOffset   uint64
	StringsOffset uint64
}

// OrbEntry represents an entry in the .orb Index Table (32 bytes on disk) plus its resolved virtual Path.
type OrbEntry struct {
	Path       string // Normalized forward-slash virtual path (e.g. "app/models/user.rb")
	PathOffset uint32
	PathLen    uint16
	Kind       OrbEntryKind
	Flags      uint8
	Hash32     uint32
	Mode       uint32
	DataOffset uint64
	DataSize   uint64
}

// IsOrbBinary reports whether data starts with a valid 32-byte ORB v1 header.
func IsOrbBinary(data []byte) bool {
	if len(data) < OrbHeaderSize {
		return false
	}
	return C.orb_is_valid_header(unsafe.Pointer(&data[0]), C.size_t(len(data))) != 0
}

// NormalizeOrbPath normalizes a path into a clean relative forward-slash virtual path.
func NormalizeOrbPath(p string) string {
	p = filepath.ToSlash(p)
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	if p == "." {
		return ""
	}
	return p
}

func orbHash32(data []byte) uint32 {
	h := fnv.New32a()
	_, _ = h.Write(data)
	return h.Sum32()
}

type orbDraftEntry struct {
	path    string
	kind    OrbEntryKind
	flags   uint8
	mode    uint32
	payload []byte
}

// OrbBuilder constructs an .orb archive from compiled .mrb bytecode, .rb sources, and assets.
type OrbBuilder struct {
	flags      uint16
	entrypoint string
	entries    map[string]*orbDraftEntry
}

// NewOrbBuilder creates a new builder for an .orb archive.
func NewOrbBuilder(flags uint16) *OrbBuilder {
	return &OrbBuilder{
		flags:   flags,
		entries: make(map[string]*orbDraftEntry),
	}
}

// AddEntry adds or replaces an entry in the bundle.
func (b *OrbBuilder) AddEntry(vpath string, kind OrbEntryKind, flags uint8, mode uint32, payload []byte) error {
	norm := NormalizeOrbPath(vpath)
	if norm == "" {
		return errors.New("orb: empty virtual path")
	}
	if len(norm) > 0xFFFF {
		return fmt.Errorf("orb: virtual path too long (%d bytes): %s", len(norm), norm)
	}
	if mode == 0 {
		if kind == OrbKindDir {
			mode = 0755
		} else {
			mode = 0644
		}
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	b.entries[norm] = &orbDraftEntry{
		path:    norm,
		kind:    kind,
		flags:   flags,
		mode:    mode,
		payload: cp,
	}
	return nil
}

// AddDir registers a virtual directory in the bundle (optionally flagged as a $LOAD_PATH root).
func (b *OrbBuilder) AddDir(vpath string, isLoadPath bool) error {
	var flags uint8
	if isLoadPath {
		flags |= OrbFlagLoadPath
	}
	return b.AddEntry(vpath, OrbKindDir, flags, 0755, nil)
}

// SetEntrypoint designates the virtual path of the default script to execute when the bundle runs.
func (b *OrbBuilder) SetEntrypoint(vpath string) {
	b.entrypoint = NormalizeOrbPath(vpath)
}

// WriteTo serializes the .orb archive to w following the 32-byte header + 4-byte aligned arena + index + string table layout.
func (b *OrbBuilder) WriteTo(w io.Writer) (int64, error) {
	keys := make([]string, 0, len(b.entries))
	for k := range b.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	entrypointIdx := OrbNoEntrypoint
	var stringTable bytes.Buffer
	indexEntries := make([]OrbEntry, len(keys))

	// First pass: calculate Data Arena offsets (starting immediately after the 32-byte header)
	curDataOffset := uint64(OrbHeaderSize)
	for i, k := range keys {
		de := b.entries[k]
		if de.flags&OrbFlagEntrypoint != 0 || (b.entrypoint != "" && (k == b.entrypoint || strings.TrimSuffix(k, path.Ext(k)) == strings.TrimSuffix(b.entrypoint, path.Ext(b.entrypoint)))) {
			de.flags |= OrbFlagEntrypoint
			if entrypointIdx == OrbNoEntrypoint {
				entrypointIdx = uint32(i)
			}
		}

		pathOff := uint32(stringTable.Len())
		stringTable.WriteString(de.path)

		var dataOff, dataSize uint64
		var h32 uint32
		if len(de.payload) > 0 {
			// Align payload to 4-byte boundary
			if rem := curDataOffset % uint64(OrbAlignment); rem != 0 {
				curDataOffset += uint64(OrbAlignment) - rem
			}
			dataOff = curDataOffset
			dataSize = uint64(len(de.payload))
			h32 = orbHash32(de.payload)
			curDataOffset += dataSize
		}

		indexEntries[i] = OrbEntry{
			Path:       de.path,
			PathOffset: pathOff,
			PathLen:    uint16(len(de.path)),
			Kind:       de.kind,
			Flags:      de.flags,
			Hash32:     h32,
			Mode:       de.mode,
			DataOffset: dataOff,
			DataSize:   dataSize,
		}
	}

	// Align Index Table start to 4-byte boundary
	if rem := curDataOffset % uint64(OrbAlignment); rem != 0 {
		curDataOffset += uint64(OrbAlignment) - rem
	}
	indexOffset := curDataOffset
	stringsOffset := indexOffset + uint64(len(indexEntries)*OrbEntrySize)

	// 1. Write 32-byte Fixed Header
	var hdrBuf [OrbHeaderSize]byte
	copy(hdrBuf[0:4], OrbMagic)
	binary.LittleEndian.PutUint16(hdrBuf[4:6], OrbVersion)
	binary.LittleEndian.PutUint16(hdrBuf[6:8], b.flags)
	binary.LittleEndian.PutUint32(hdrBuf[8:12], uint32(len(indexEntries)))
	binary.LittleEndian.PutUint32(hdrBuf[12:16], entrypointIdx)
	binary.LittleEndian.PutUint64(hdrBuf[16:24], indexOffset)
	binary.LittleEndian.PutUint64(hdrBuf[24:32], stringsOffset)

	var totalWritten int64
	n, err := w.Write(hdrBuf[:])
	totalWritten += int64(n)
	if err != nil {
		return totalWritten, err
	}

	var padBuf [OrbAlignment]byte
	writePad := func(targetOffset uint64) error {
		if uint64(totalWritten) < targetOffset {
			padLen := int(targetOffset - uint64(totalWritten))
			wn, werr := w.Write(padBuf[:padLen])
			totalWritten += int64(wn)
			return werr
		}
		return nil
	}

	// 2. Write 4-byte aligned Data Arena payloads
	for i, k := range keys {
		de := b.entries[k]
		if len(de.payload) == 0 {
			continue
		}
		if err := writePad(indexEntries[i].DataOffset); err != nil {
			return totalWritten, err
		}
		wn, werr := w.Write(de.payload)
		totalWritten += int64(wn)
		if werr != nil {
			return totalWritten, werr
		}
	}

	if err := writePad(indexOffset); err != nil {
		return totalWritten, err
	}

	// 3. Write Fixed-Size Index Table (32 bytes per entry)
	var entBuf [OrbEntrySize]byte
	for _, ie := range indexEntries {
		binary.LittleEndian.PutUint32(entBuf[0:4], ie.PathOffset)
		binary.LittleEndian.PutUint16(entBuf[4:6], ie.PathLen)
		entBuf[6] = uint8(ie.Kind)
		entBuf[7] = ie.Flags
		binary.LittleEndian.PutUint32(entBuf[8:12], ie.Hash32)
		binary.LittleEndian.PutUint32(entBuf[12:16], ie.Mode)
		binary.LittleEndian.PutUint64(entBuf[16:24], ie.DataOffset)
		binary.LittleEndian.PutUint64(entBuf[24:32], ie.DataSize)

		wn, werr := w.Write(entBuf[:])
		totalWritten += int64(wn)
		if werr != nil {
			return totalWritten, werr
		}
	}

	// 4. Write Contiguous String Table
	wn, werr := w.Write(stringTable.Bytes())
	totalWritten += int64(wn)
	return totalWritten, werr
}

// Bytes serializes the .orb archive into a byte slice.
func (b *OrbBuilder) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if _, err := b.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteFile writes the .orb archive to disk at filename.
func (b *OrbBuilder) WriteFile(filename string) error {
	data, err := b.Bytes()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// OrbFile represents a parsed, read-only .orb archive in memory.
type OrbFile struct {
	Name      string // Real path on disk (or identifier)
	MountRoot string // Virtual mount root (e.g. "/@orb/0")
	Header    OrbHeader
	Entries   []OrbEntry
	byPath    map[string]int
	raw       []byte
	aotFuncs  map[string]func(*MrbState) (Value, error)
}

// RegisterAotFunc attaches a compiled native Go AOT function to a virtual entry path in the OrbFile.
func (o *OrbFile) RegisterAotFunc(vpath string, fn func(*MrbState) (Value, error)) {
	if o == nil || fn == nil {
		return
	}
	if o.aotFuncs == nil {
		o.aotFuncs = make(map[string]func(*MrbState) (Value, error))
	}
	norm := NormalizeOrbPath(vpath)
	o.aotFuncs[norm] = fn
	if strings.HasSuffix(norm, ".rb") {
		o.aotFuncs[strings.TrimSuffix(norm, ".rb")+".mrb"] = fn
	} else if strings.HasSuffix(norm, ".mrb") {
		o.aotFuncs[strings.TrimSuffix(norm, ".mrb")+".rb"] = fn
	}
}

// ReadOrbFile reads and parses an .orb archive from disk.
func ReadOrbFile(filename string) (*OrbFile, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(filename)
	if err == nil {
		if real, rerr := filepath.EvalSymlinks(abs); rerr == nil {
			abs = real
		}
		filename = abs
	}
	return ParseOrb(filename, data)
}

// ParseOrb parses an in-memory .orb binary image.
func ParseOrb(name string, data []byte) (*OrbFile, error) {
	if !IsOrbBinary(data) {
		return nil, fmt.Errorf("orb: invalid ORB header in %s", name)
	}
	var hdr OrbHeader
	copy(hdr.Magic[:], data[0:4])
	hdr.Version = binary.LittleEndian.Uint16(data[4:6])
	hdr.Flags = binary.LittleEndian.Uint16(data[6:8])
	hdr.EntryCount = binary.LittleEndian.Uint32(data[8:12])
	hdr.EntrypointIdx = binary.LittleEndian.Uint32(data[12:16])
	hdr.IndexOffset = binary.LittleEndian.Uint64(data[16:24])
	hdr.StringsOffset = binary.LittleEndian.Uint64(data[24:32])

	fileLen := uint64(len(data))
	indexEnd := hdr.IndexOffset + uint64(hdr.EntryCount)*uint64(OrbEntrySize)
	if hdr.IndexOffset < uint64(OrbHeaderSize) || indexEnd > fileLen || hdr.StringsOffset < indexEnd || hdr.StringsOffset > fileLen {
		return nil, fmt.Errorf("orb: corrupted section offsets in %s", name)
	}

	stringsData := data[hdr.StringsOffset:]
	entries := make([]OrbEntry, hdr.EntryCount)
	byPath := make(map[string]int, hdr.EntryCount)

	for i := uint32(0); i < hdr.EntryCount; i++ {
		off := hdr.IndexOffset + uint64(i)*uint64(OrbEntrySize)
		eb := data[off : off+uint64(OrbEntrySize)]

		pOff := binary.LittleEndian.Uint32(eb[0:4])
		pLen := binary.LittleEndian.Uint16(eb[4:6])
		if uint64(pOff)+uint64(pLen) > uint64(len(stringsData)) {
			return nil, fmt.Errorf("orb: entry %d string out of bounds in %s", i, name)
		}
		vpath := string(stringsData[pOff : uint32(pOff)+uint32(pLen)])
		dOff := binary.LittleEndian.Uint64(eb[16:24])
		dSize := binary.LittleEndian.Uint64(eb[24:32])
		if dSize > 0 && (dOff < uint64(OrbHeaderSize) || dOff+dSize > hdr.IndexOffset) {
			return nil, fmt.Errorf("orb: entry %d (%s) payload out of bounds in %s", i, vpath, name)
		}

		entries[i] = OrbEntry{
			Path:       vpath,
			PathOffset: pOff,
			PathLen:    pLen,
			Kind:       OrbEntryKind(eb[6]),
			Flags:      eb[7],
			Hash32:     binary.LittleEndian.Uint32(eb[8:12]),
			Mode:       binary.LittleEndian.Uint32(eb[12:16]),
			DataOffset: dOff,
			DataSize:   dSize,
		}
		byPath[vpath] = int(i)
	}

	return &OrbFile{
		Name:    name,
		Header:  hdr,
		Entries: entries,
		byPath:  byPath,
		raw:     data,
	}, nil
}

// Entrypoint returns the designated entrypoint entry of the bundle, if any.
func (o *OrbFile) Entrypoint() (*OrbEntry, bool) {
	if o == nil || o.Header.EntrypointIdx == OrbNoEntrypoint || o.Header.EntrypointIdx >= uint32(len(o.Entries)) {
		return nil, false
	}
	return &o.Entries[o.Header.EntrypointIdx], true
}

// Lookup finds an entry by its exact normalized virtual path (or .rb/.mrb sibling).
func (o *OrbFile) Lookup(vpath string) (*OrbEntry, bool) {
	if o == nil {
		return nil, false
	}
	norm := NormalizeOrbPath(vpath)
	if idx, ok := o.byPath[norm]; ok {
		return &o.Entries[idx], true
	}
	if strings.HasSuffix(norm, ".mrb") {
		if idx, ok := o.byPath[strings.TrimSuffix(norm, ".mrb")+".rb"]; ok {
			return &o.Entries[idx], true
		}
	} else if strings.HasSuffix(norm, ".rb") {
		if idx, ok := o.byPath[strings.TrimSuffix(norm, ".rb")+".mrb"]; ok {
			return &o.Entries[idx], true
		}
	}
	return nil, false
}

// EntryData returns a zero-copy slice of the entry's payload bytes.
func (o *OrbFile) EntryData(e *OrbEntry) []byte {
	if o == nil || e == nil || e.DataSize == 0 {
		return nil
	}
	return o.raw[e.DataOffset : e.DataOffset+e.DataSize]
}

// VirtualPath returns the absolute virtual path of entry e under the bundle's MountRoot.
func (o *OrbFile) VirtualPath(e *OrbEntry) string {
	if o == nil || e == nil {
		return ""
	}
	root := o.MountRoot
	if root == "" {
		root = OrbVirtualRoot + "/0"
	}
	return root + "/" + e.Path
}

// VirtualLoadPaths returns the virtual directories that should be added to $LOAD_PATH when mounted.
func (o *OrbFile) VirtualLoadPaths() []string {
	if o == nil {
		return nil
	}
	root := o.MountRoot
	if root == "" {
		root = OrbVirtualRoot + "/0"
	}
	seen := map[string]bool{root: true}
	out := []string{root}

	addDir := func(rel string) {
		rel = NormalizeOrbPath(rel)
		if rel == "" {
			return
		}
		vp := root + "/" + rel
		if !seen[vp] {
			seen[vp] = true
			out = append(out, vp)
		}
	}

	hasLib := false
	for i := range o.Entries {
		e := &o.Entries[i]
		if e.Flags&OrbFlagLoadPath != 0 {
			if e.Kind == OrbKindDir {
				addDir(e.Path)
			} else {
				addDir(path.Dir(e.Path))
			}
		}
		if e.Path == "lib" || strings.HasPrefix(e.Path, "lib/") {
			hasLib = true
		}
	}
	if hasLib {
		addDir("lib")
	}
	return out
}

const orbRegistryFeatureKey = "__oruby_orb_registry__"

type orbRegistry struct {
	mounts []*OrbFile
	byName map[string]*OrbFile
	byRoot map[string]*OrbFile
}

func (mrb *MrbState) getOrbRegistry() *orbRegistry {
	if reg, ok := mrb.features[orbRegistryFeatureKey].(*orbRegistry); ok && reg != nil {
		return reg
	}
	reg := &orbRegistry{
		byName: make(map[string]*OrbFile),
		byRoot: make(map[string]*OrbFile),
	}
	mrb.features[orbRegistryFeatureKey] = reg
	return reg
}

// MountedOrbs returns all .orb bundles currently mounted in mrb.
func (mrb *MrbState) MountedOrbs() []*OrbFile {
	if reg, ok := mrb.features[orbRegistryFeatureKey].(*orbRegistry); ok && reg != nil {
		return reg.mounts
	}
	return nil
}

// IsOrbVirtualPath reports whether p is a virtual path inside a mounted .orb bundle ("/@orb/...").
func IsOrbVirtualPath(p string) bool {
	s := filepath.ToSlash(p)
	return s == OrbVirtualRoot || strings.HasPrefix(s, OrbVirtualRoot+"/")
}

// MountOrbFile reads an .orb archive from disk and mounts it into mrb (idempotent by real file path).
func (mrb *MrbState) MountOrbFile(filename string) (*OrbFile, error) {
	abs := filename
	if a, err := filepath.Abs(filename); err == nil {
		if r, rerr := filepath.EvalSymlinks(a); rerr == nil {
			abs = r
		} else {
			abs = a
		}
	}
	reg := mrb.getOrbRegistry()
	if existing, ok := reg.byName[abs]; ok {
		return existing, nil
	}
	orb, err := ReadOrbFile(abs)
	if err != nil {
		return nil, err
	}
	return mrb.MountOrb(orb)
}

// MountOrbBytes parses and mounts an in-memory .orb archive into mrb.
func (mrb *MrbState) MountOrbBytes(name string, data []byte) (*OrbFile, error) {
	abs := name
	if name != "" && name != "-" && !IsOrbVirtualPath(name) {
		if a, err := filepath.Abs(name); err == nil {
			if r, rerr := filepath.EvalSymlinks(a); rerr == nil {
				abs = r
			} else {
				abs = a
			}
		}
	}
	reg := mrb.getOrbRegistry()
	if abs != "" {
		if existing, ok := reg.byName[abs]; ok {
			return existing, nil
		}
	}
	orb, err := ParseOrb(abs, data)
	if err != nil {
		return nil, err
	}
	return mrb.MountOrb(orb)
}

// MountOrb mounts a parsed OrbFile into mrb and registers its virtual load paths in $LOAD_PATH.
func (mrb *MrbState) MountOrb(orb *OrbFile) (*OrbFile, error) {
	if orb == nil {
		return nil, errors.New("orb: cannot mount nil OrbFile")
	}
	reg := mrb.getOrbRegistry()
	if orb.Name != "" {
		if existing, ok := reg.byName[orb.Name]; ok {
			return existing, nil
		}
	}
	cp := *orb
	cp.MountRoot = fmt.Sprintf("%s/%d", OrbVirtualRoot, len(reg.mounts))
	reg.mounts = append(reg.mounts, &cp)
	if cp.Name != "" {
		reg.byName[cp.Name] = &cp
	}
	reg.byRoot[cp.MountRoot] = &cp

	// Map each code entry's relative debug filename to its virtual path in
	// Kernel's @__oruby_source_paths so caller_locations#absolute_path and
	// require_relative resolve inside the mounted .orb bundle.
	kernel := mrb.KernelModule()
	paths := mrb.IVGet(kernel, Sym___oruby_source_paths)
	if paths.Type() != MrbTTHash {
		paths = mrb.HashNew().Value()
		_ = mrb.IVSet(kernel, Sym___oruby_source_paths, paths)
	}
	for i := range cp.Entries {
		e := &cp.Entries[i]
		if e.Kind == OrbKindMrb || e.Kind == OrbKindRuby {
			vp := mrb.StrNew(cp.VirtualPath(e))
			mrb.HashSet(paths, mrb.SymbolValue(mrb.Intern(e.Path)), vp)
		}
	}

	// Add virtual load paths to $LOAD_PATH if $LOAD_PATH exists
	lpv := mrb.GVGet(GvSym_LOAD_PATH)
	if lpv.IsArray() {
		existing := make(map[string]bool)
		for i := 0; i < RArrayLen(lpv); i++ {
			ev := mrb.AryEntry(lpv, i)
			if ev.IsString() {
				existing[mrb.String(ev)] = true
			}
		}
		for _, vp := range cp.VirtualLoadPaths() {
			if !existing[vp] {
				mrb.AryPush(lpv, mrb.StrNew(vp))
			}
		}
		// Prepend the bundle's virtual lib directory so its packed bundler/setup
		// takes precedence over the host's default bundler when running self-contained.
		vlib := cp.MountRoot + "/lib"
		if existing[vlib] || cp.hasLibDir() {
			mrb.AryUnshift(lpv, mrb.StrNew(vlib))
		}
	}
	return &cp, nil
}

func (o *OrbFile) hasLibDir() bool {
	if o == nil {
		return false
	}
	for i := range o.Entries {
		p := o.Entries[i].Path
		if p == "lib" || strings.HasPrefix(p, "lib/") {
			return true
		}
	}
	return false
}

// OrbLookupVirtual looks up a virtual path ("/@orb/<id>/<relpath>") or relative path in mounted .orb archives.
func (mrb *MrbState) OrbLookupVirtual(p string) (*OrbEntry, *OrbFile, bool) {
	reg, ok := mrb.features[orbRegistryFeatureKey].(*orbRegistry)
	if !ok || reg == nil || len(reg.mounts) == 0 {
		return nil, nil, false
	}
	slash := filepath.ToSlash(p)
	if strings.HasPrefix(slash, OrbVirtualRoot+"/") {
		rest := strings.TrimPrefix(slash, OrbVirtualRoot+"/")
		slashIdx := strings.IndexByte(rest, '/')
		if slashIdx < 0 {
			return nil, nil, false
		}
		root := OrbVirtualRoot + "/" + rest[:slashIdx]
		rel := rest[slashIdx+1:]
		if orb, exists := reg.byRoot[root]; exists {
			if entry, found := orb.Lookup(rel); found && (entry.Kind == OrbKindMrb || entry.Kind == OrbKindRuby) {
				return entry, orb, true
			}
		}
		return nil, nil, false
	}
	return nil, nil, false
}

// OrbReadAsset reads any entry (including KindAsset) by virtual path ("/@orb/<id>/<path>") or relative path across mounted .orb bundles.
func (mrb *MrbState) OrbReadAsset(p string) ([]byte, bool) {
	reg, ok := mrb.features[orbRegistryFeatureKey].(*orbRegistry)
	if !ok || reg == nil || len(reg.mounts) == 0 {
		return nil, false
	}
	slash := filepath.ToSlash(p)
	if strings.HasPrefix(slash, OrbVirtualRoot+"/") {
		rest := strings.TrimPrefix(slash, OrbVirtualRoot+"/")
		if slashIdx := strings.IndexByte(rest, '/'); slashIdx >= 0 {
			root := OrbVirtualRoot + "/" + rest[:slashIdx]
			rel := rest[slashIdx+1:]
			if orb, exists := reg.byRoot[root]; exists {
				if entry, found := orb.Lookup(rel); found && entry.Kind != OrbKindDir {
					return orb.EntryData(entry), true
				}
				if diskPath, ok := orb.diskFallbackPath(rel); ok {
					if data, err := os.ReadFile(diskPath); err == nil {
						return data, true
					}
				}
			}
		}
		return nil, false
	}
	norm := NormalizeOrbPath(slash)
	for _, orb := range reg.mounts {
		if entry, found := orb.Lookup(norm); found && entry.Kind != OrbKindDir {
			return orb.EntryData(entry), true
		}
	}
	return nil, false
}

// diskFallbackPath returns a host filesystem path for standalone external directories
// (such as "public/...") when an .orb bundle is run next to a standalone public/ folder.
func (o *OrbFile) diskFallbackPath(rel string) (string, bool) {
	rel = NormalizeOrbPath(rel)
	if rel != "public" && !strings.HasPrefix(rel, "public/") {
		return "", false
	}
	fromSlash := filepath.FromSlash(rel)
	if cwd, err := os.Getwd(); err == nil {
		cand := filepath.Join(cwd, fromSlash)
		if _, err := os.Stat(cand); err == nil {
			return cand, true
		}
	}
	if o != nil && o.Name != "" && !IsOrbVirtualPath(o.Name) {
		base := filepath.Dir(o.Name)
		cand := filepath.Join(base, fromSlash)
		if _, err := os.Stat(cand); err == nil {
			return cand, true
		}
		// Also check parent dir if .orb is inside build/ (e.g. build/app.orb -> ../public)
		parentCand := filepath.Join(filepath.Dir(base), fromSlash)
		if _, err := os.Stat(parentCand); err == nil {
			return parentCand, true
		}
	}
	return "", false
}

type orbFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (fi *orbFileInfo) Name() string       { return fi.name }
func (fi *orbFileInfo) Size() int64        { return fi.size }
func (fi *orbFileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *orbFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *orbFileInfo) IsDir() bool        { return fi.mode.IsDir() }
func (fi *orbFileInfo) Sys() interface{}   { return nil }

type orbDirEntry struct {
	info *orbFileInfo
}

func (de *orbDirEntry) Name() string               { return de.info.Name() }
func (de *orbDirEntry) IsDir() bool                { return de.info.IsDir() }
func (de *orbDirEntry) Type() os.FileMode          { return de.info.Mode().Type() }
func (de *orbDirEntry) Info() (os.FileInfo, error) { return de.info, nil }

// OrbStat returns os.FileInfo for a virtual path ("/@orb/..." or "/@orb/<id>/...") inside mounted .orb bundles.
func (mrb *MrbState) OrbStat(p string) (os.FileInfo, error) {
	slash := path.Clean(filepath.ToSlash(p))
	if !IsOrbVirtualPath(slash) {
		return nil, &os.PathError{Op: "stat", Path: p, Err: syscall.ENOENT}
	}
	reg, ok := mrb.features[orbRegistryFeatureKey].(*orbRegistry)
	if !ok || reg == nil || len(reg.mounts) == 0 {
		return nil, &os.PathError{Op: "stat", Path: p, Err: syscall.ENOENT}
	}
	now := time.Unix(1700000000, 0)
	if slash == OrbVirtualRoot {
		return &orbFileInfo{
			name:    "@orb",
			mode:    os.ModeDir | 0755,
			modTime: now,
		}, nil
	}
	rest := strings.TrimPrefix(slash, OrbVirtualRoot+"/")
	slashIdx := strings.IndexByte(rest, '/')
	if slashIdx < 0 {
		root := OrbVirtualRoot + "/" + rest
		if _, exists := reg.byRoot[root]; exists {
			return &orbFileInfo{
				name:    rest,
				mode:    os.ModeDir | 0755,
				modTime: now,
			}, nil
		}
		return nil, &os.PathError{Op: "stat", Path: p, Err: syscall.ENOENT}
	}
	root := OrbVirtualRoot + "/" + rest[:slashIdx]
	rel := NormalizeOrbPath(rest[slashIdx+1:])
	orb, exists := reg.byRoot[root]
	if !exists || orb == nil {
		return nil, &os.PathError{Op: "stat", Path: p, Err: syscall.ENOENT}
	}
	if rel == "" {
		return &orbFileInfo{
			name:    path.Base(root),
			mode:    os.ModeDir | 0755,
			modTime: now,
		}, nil
	}
	// Check exact match in byPath first, then .rb <-> .mrb fallback via Lookup
	if idx, found := orb.byPath[rel]; found {
		e := &orb.Entries[idx]
		mode := os.FileMode(e.Mode & 0777)
		if mode == 0 {
			mode = 0644
		}
		if e.Kind == OrbKindDir {
			mode = os.ModeDir | 0755
		}
		return &orbFileInfo{
			name:    path.Base(rel),
			size:    int64(e.DataSize),
			mode:    mode,
			modTime: now,
		}, nil
	}
	if e, found := orb.Lookup(rel); found {
		mode := os.FileMode(e.Mode & 0777)
		if mode == 0 {
			mode = 0644
		}
		if e.Kind == OrbKindDir {
			mode = os.ModeDir | 0755
		}
		return &orbFileInfo{
			name:    path.Base(rel),
			size:    int64(e.DataSize),
			mode:    mode,
			modTime: now,
		}, nil
	}
	// Check implicit directory (any entry having rel+"/" as prefix)
	prefix := rel + "/"
	for i := range orb.Entries {
		if strings.HasPrefix(orb.Entries[i].Path, prefix) {
			return &orbFileInfo{
				name:    path.Base(rel),
				mode:    os.ModeDir | 0755,
				modTime: now,
			}, nil
		}
	}
	if diskPath, ok := orb.diskFallbackPath(rel); ok {
		if fi, err := os.Stat(diskPath); err == nil {
			return fi, nil
		}
	}
	return nil, &os.PathError{Op: "stat", Path: p, Err: syscall.ENOENT}
}

// OrbReadDir returns sorted directory entries for a virtual directory path ("/@orb/..." or "/@orb/<id>/...").
func (mrb *MrbState) OrbReadDir(p string) ([]os.DirEntry, error) {
	slash := path.Clean(filepath.ToSlash(p))
	if !IsOrbVirtualPath(slash) {
		return nil, &os.PathError{Op: "open", Path: p, Err: syscall.ENOENT}
	}
	reg, ok := mrb.features[orbRegistryFeatureKey].(*orbRegistry)
	if !ok || reg == nil || len(reg.mounts) == 0 {
		return nil, &os.PathError{Op: "open", Path: p, Err: syscall.ENOENT}
	}
	now := time.Unix(1700000000, 0)
	if slash == OrbVirtualRoot {
		out := make([]os.DirEntry, 0, len(reg.mounts))
		for _, m := range reg.mounts {
			name := strings.TrimPrefix(m.MountRoot, OrbVirtualRoot+"/")
			out = append(out, &orbDirEntry{
				info: &orbFileInfo{
					name:    name,
					mode:    os.ModeDir | 0755,
					modTime: now,
				},
			})
		}
		return out, nil
	}

	rest := strings.TrimPrefix(slash, OrbVirtualRoot+"/")
	var root, rel string
	if slashIdx := strings.IndexByte(rest, '/'); slashIdx >= 0 {
		root = OrbVirtualRoot + "/" + rest[:slashIdx]
		rel = NormalizeOrbPath(rest[slashIdx+1:])
	} else {
		root = OrbVirtualRoot + "/" + rest
	}
	orb, exists := reg.byRoot[root]
	if !exists || orb == nil {
		return nil, &os.PathError{Op: "open", Path: p, Err: syscall.ENOENT}
	}

	prefix := ""
	if rel != "" {
		if idx, found := orb.byPath[rel]; found && orb.Entries[idx].Kind != OrbKindDir {
			return nil, &os.PathError{Op: "readdirent", Path: p, Err: syscall.ENOTDIR}
		}
		prefix = rel + "/"
	}

	children := make(map[string]*orbFileInfo)
	foundDir := rel == ""
	if !foundDir {
		if idx, found := orb.byPath[rel]; found && orb.Entries[idx].Kind == OrbKindDir {
			foundDir = true
		}
	}

	for i := range orb.Entries {
		e := &orb.Entries[i]
		if prefix != "" {
			if !strings.HasPrefix(e.Path, prefix) {
				continue
			}
		}
		foundDir = true
		sub := strings.TrimPrefix(e.Path, prefix)
		if sub == "" {
			continue
		}
		if slashIdx := strings.IndexByte(sub, '/'); slashIdx >= 0 {
			dirName := sub[:slashIdx]
			children[dirName] = &orbFileInfo{
				name:    dirName,
				mode:    os.ModeDir | 0755,
				modTime: now,
			}
		} else {
			// If a file is stored as .mrb, expose it as .rb in directory listings
			// so Zeitwerk and Rails initializers/routes scanners see standard .rb filenames!
			childName := sub
			if e.Kind == OrbKindMrb && strings.HasSuffix(childName, ".mrb") {
				childName = strings.TrimSuffix(childName, ".mrb") + ".rb"
			}
			if existing, exists := children[childName]; exists && existing.IsDir() {
				continue
			}
			mode := os.FileMode(e.Mode & 0777)
			if mode == 0 {
				mode = 0644
			}
			if e.Kind == OrbKindDir {
				mode = os.ModeDir | 0755
			}
			children[childName] = &orbFileInfo{
				name:    childName,
				size:    int64(e.DataSize),
				mode:    mode,
				modTime: now,
			}
		}
	}

	if !foundDir {
		if diskPath, ok := orb.diskFallbackPath(rel); ok {
			if entries, err := os.ReadDir(diskPath); err == nil {
				return entries, nil
			}
		}
		return nil, &os.PathError{Op: "open", Path: p, Err: syscall.ENOENT}
	}

	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]os.DirEntry, len(names))
	for i, name := range names {
		out[i] = &orbDirEntry{info: children[name]}
	}
	return out, nil
}

// ResolveEntrypoint determines which entry in orb should be executed when required/loaded.
// It checks Header.EntrypointIdx first, then matches featureHint (e.g. "foo" -> "lib/foo.rb" or "foo.rb").
func (o *OrbFile) ResolveEntrypoint(featureHint string) (*OrbEntry, bool) {
	if ep, ok := o.Entrypoint(); ok {
		return ep, true
	}
	if featureHint == "" {
		return nil, false
	}
	base := path.Base(filepath.ToSlash(featureHint))
	base = strings.TrimSuffix(base, path.Ext(base))
	if base == "" {
		return nil, false
	}
	for _, cand := range []string{
		"lib/" + base + ".rb",
		base + ".rb",
		"lib/" + base + ".mrb",
		base + ".mrb",
	} {
		if e, ok := o.Lookup(cand); ok && (e.Kind == OrbKindMrb || e.Kind == OrbKindRuby) {
			return e, true
		}
	}
	return nil, false
}

// ExecuteOrbEntry executes a single OrbEntry within mrb using its virtual path as __FILE__.
func (mrb *MrbState) ExecuteOrbEntry(orb *OrbFile, entry *OrbEntry, cxt *MrbCcontext) (Value, error) {
	if orb == nil || entry == nil {
		return mrb.NilValue(), errors.New("orb: nil entry")
	}
	if cxt == nil {
		cxt = mrb.MrbCcontextNew()
		defer cxt.Free()
	}
	vpath := orb.VirtualPath(entry)
	cxt.Filename(vpath)
	cxt.SetCaptureErrors(true)

	prevLoader := mrb.GetGV(`$"__`)
	mrb.SetGV(`$"__`, mrb.StrNew(vpath))
	defer mrb.SetGV(`$"__`, prevLoader)

	if orb.aotFuncs != nil {
		if fn, ok := orb.aotFuncs[NormalizeOrbPath(entry.Path)]; ok && fn != nil {
			sc := mrb.AotScopePush(mrb.ObjectClass().Value(), true)
			res, err := fn(mrb)
			mrb.AotScopePop(sc)
			if err != nil {
				mrb.AotLocalJumpCheck()
				return res, mrb.AotBoundaryErr()
			}
			return res, nil
		}
	}

	data := orb.EntryData(entry)
	switch entry.Kind {
	case OrbKindMrb:
		return mrb.LoadIrepCxt(data, cxt)
	case OrbKindRuby:
		return mrb.LoadBytesCxt(data, cxt)
	default:
		return mrb.NilValue(), fmt.Errorf("orb: entry %s is not executable code (kind=%d)", entry.Path, entry.Kind)
	}
}

// ExecuteOrbEntryWrapped executes a single OrbEntry wrapped in anonymous module mod (for Kernel#load(file, wrap)).
func (mrb *MrbState) ExecuteOrbEntryWrapped(orb *OrbFile, entry *OrbEntry, mod, self Value) (Value, error) {
	if orb == nil || entry == nil {
		return mrb.NilValue(), errors.New("orb: nil entry")
	}
	vpath := orb.VirtualPath(entry)
	cxt := mrb.MrbCcontextNew()
	defer cxt.Free()
	cxt.Filename(vpath)

	prevLoader := mrb.GetGV(`$"__`)
	mrb.SetGV(`$"__`, mrb.StrNew(vpath))
	defer mrb.SetGV(`$"__`, prevLoader)

	if orb.aotFuncs != nil {
		if fn, ok := orb.aotFuncs[NormalizeOrbPath(entry.Path)]; ok && fn != nil {
			sc := mrb.AotScopePush(mrb.ObjectClass().Value(), true)
			res, err := fn(mrb)
			mrb.AotScopePop(sc)
			if err != nil {
				mrb.AotLocalJumpCheck()
				return res, mrb.AotBoundaryErr()
			}
			return res, nil
		}
	}

	data := orb.EntryData(entry)
	switch entry.Kind {
	case OrbKindMrb:
		return mrb.LoadIrepBytesWrapped(data, cxt, mrb.ClassPtr(mod), self)
	case OrbKindRuby:
		return mrb.LoadBytesWrapped(data, cxt, mrb.ClassPtr(mod), self)
	default:
		return mrb.NilValue(), fmt.Errorf("orb: entry %s is not executable code (kind=%d)", entry.Path, entry.Kind)
	}
}

// OrbRelativePath is the virtual path of a mounted bundle's entry named by
// a relative path p that is no file on disk: the file name a bundle's code
// was compiled under, which __FILE__ gives (`File.open(__FILE__)`, as Logger
// does).
func (mrb *MrbState) OrbRelativePath(p string) (string, bool) {
	if p == "" || filepath.IsAbs(p) || IsOrbVirtualPath(p) {
		return "", false
	}
	mounts := mrb.MountedOrbs()
	if len(mounts) == 0 {
		return "", false
	}
	if _, err := os.Stat(p); err == nil {
		return "", false
	}
	for _, o := range mounts {
		if e, ok := o.Lookup(p); ok && e.Kind != OrbKindDir {
			return o.VirtualPath(e), true
		}
	}
	return "", false
}
