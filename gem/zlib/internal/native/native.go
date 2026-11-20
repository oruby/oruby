// Package native binds ORuby to the vendored zlib streams and checksums.
package native

/*
#cgo CFLAGS: -DZ_PREFIX
#include <stdlib.h>
#include "zutil.h"

typedef struct {
	z_stream z;
	gz_header *header;
	int deflating;
} oruby_zstream;

static void oruby_free_header(gz_header *header) {
	if (header != NULL) {
		free(header->name);
		free(header->comment);
		free(header);
	}
}

static oruby_zstream *oruby_new_stream(int deflating, int level, int window,
                                     int mem, int strategy, int *status) {
	oruby_zstream *s = calloc(1, sizeof(*s));
	if (s == NULL) {
		*status = Z_MEM_ERROR;
		return NULL;
	}
	s->deflating = deflating;
	*status = deflating ? z_deflateInit2(&s->z, level, Z_DEFLATED, window, mem, strategy)
	                    : z_inflateInit2(&s->z, window);
	if (*status != Z_OK) {
		free(s);
		return NULL;
	}
	return s;
}

static void oruby_free_stream(oruby_zstream *s) {
	if (s->deflating)
		deflateEnd(&s->z);
	else
		inflateEnd(&s->z);
	oruby_free_header(s->header);
	free(s);
}

static oruby_zstream *oruby_copy_deflate(oruby_zstream *src, int *status) {
	// deflateCopy shares the optional gzip header. Only GzipWriter installs
	// one, and its compressor is private to that writer.
	if (!src->deflating || src->header != NULL) {
		*status = Z_STREAM_ERROR;
		return NULL;
	}
	oruby_zstream *dst = calloc(1, sizeof(*dst));
	if (dst == NULL) {
		*status = Z_MEM_ERROR;
		return NULL;
	}
	*status = deflateCopy(&dst->z, &src->z);
	if (*status != Z_OK) {
		free(dst);
		return NULL;
	}
	dst->deflating = 1;
	return dst;
}

static int oruby_set_header(oruby_zstream *s, char *name, char *comment, uLong mtime, int os) {
	if (!s->deflating) {
		free(name);
		free(comment);
		return Z_STREAM_ERROR;
	}
	gz_header *header = calloc(1, sizeof(*header));
	if (header == NULL) {
		free(name);
		free(comment);
		return Z_MEM_ERROR;
	}
	header->name = (Bytef *)name;
	header->comment = (Bytef *)comment;
	header->time = mtime;
	header->os = os;
	int status = deflateSetHeader(&s->z, header);
	if (status == Z_OK) {
		oruby_free_header(s->header);
		s->header = header;
	} else {
		oruby_free_header(header);
	}
	return status;
}

typedef struct {
	unsigned in;
	unsigned out;
	int status;
} oruby_step;

// zlib only borrows the Go buffers for the duration of these calls.
static oruby_step oruby_run(oruby_zstream *s, Bytef *in, unsigned in_len,
                            Bytef *out, unsigned out_len, int flush) {
	s->z.next_in = in;
	s->z.avail_in = in_len;
	s->z.next_out = out;
	s->z.avail_out = out_len;
	int status = s->deflating ? deflate(&s->z, flush) : inflate(&s->z, flush);
	oruby_step step = { in_len - s->z.avail_in, out_len - s->z.avail_out, status };
	s->z.next_in = Z_NULL;
	s->z.next_out = Z_NULL;
	s->z.avail_in = s->z.avail_out = 0;
	return step;
}

static oruby_step oruby_params(oruby_zstream *s, Bytef *out, unsigned out_len,
                               int level, int strategy) {
	if (!s->deflating) {
		oruby_step step = { 0, 0, Z_STREAM_ERROR };
		return step;
	}
	s->z.next_out = out;
	s->z.avail_out = out_len;
	int status = deflateParams(&s->z, level, strategy);
	oruby_step step = { 0, out_len - s->z.avail_out, status };
	s->z.next_out = Z_NULL;
	s->z.avail_out = 0;
	return step;
}

static int oruby_sync(oruby_zstream *s, Bytef *in, unsigned in_len, unsigned *used) {
	if (s->deflating)
		return Z_STREAM_ERROR;
	s->z.next_in = in;
	s->z.avail_in = in_len;
	int status = inflateSync(&s->z);
	*used = in_len - s->z.avail_in;
	s->z.next_in = Z_NULL;
	s->z.avail_in = 0;
	return status;
}

static int oruby_sync_point(oruby_zstream *s) {
	return s->deflating ? 0 : inflateSyncPoint(&s->z);
}

static int oruby_reset(oruby_zstream *s) {
	return s->deflating ? deflateReset(&s->z) : inflateReset(&s->z);
}

static int oruby_dictionary(oruby_zstream *s, Bytef *dict, unsigned len) {
	// zlib requires a non-null pointer even for an empty dictionary.
	Bytef empty = 0;
	if (dict == Z_NULL)
		dict = &empty;
	return s->deflating ? deflateSetDictionary(&s->z, dict, len)
	                    : inflateSetDictionary(&s->z, dict, len);
}
*/
import "C"

import (
	"math"
	"runtime"
	"unsafe"
)

const (
	OK           = C.Z_OK
	StreamEnd    = C.Z_STREAM_END
	NeedDict     = C.Z_NEED_DICT
	StreamError  = C.Z_STREAM_ERROR
	DataError    = C.Z_DATA_ERROR
	MemError     = C.Z_MEM_ERROR
	BufError     = C.Z_BUF_ERROR
	VersionError = C.Z_VERSION_ERROR

	NoFlush = C.Z_NO_FLUSH
	Finish  = C.Z_FINISH
)

type Stream struct {
	ptr     *C.oruby_zstream
	cleanup runtime.Cleanup
}

func NewDeflate(level, window, mem, strategy int) (*Stream, int) {
	return newStream(1, level, window, mem, strategy)
}

func NewInflate(window int) (*Stream, int) {
	return newStream(0, 0, window, 0, 0)
}

func newStream(deflating, level, window, mem, strategy int) (*Stream, int) {
	var status C.int
	p := C.oruby_new_stream(C.int(deflating), C.int(level), C.int(window), C.int(mem), C.int(strategy), &status)
	if p == nil {
		return nil, int(status)
	}
	return wrapStream(p), OK
}

func wrapStream(p *C.oruby_zstream) *Stream {
	s := &Stream{ptr: p}
	s.cleanup = runtime.AddCleanup(s, freeStream, p)
	return s
}

func freeStream(p *C.oruby_zstream) {
	C.oruby_free_stream(p)
}

func (s *Stream) Close() {
	if s.ptr == nil {
		return
	}
	s.cleanup.Stop()
	freeStream(s.ptr)
	s.ptr = nil
}

type State struct {
	TotalIn  uint64
	TotalOut uint64
	Adler    uint32
	DataType int
}

func (s *Stream) State() State {
	z := &s.ptr.z
	state := State{uint64(z.total_in), uint64(z.total_out), uint32(z.adler), int(z.data_type)}
	runtime.KeepAlive(s)
	return state
}

func (s *Stream) Message() string {
	msg := C.GoString(s.ptr.z.msg)
	runtime.KeepAlive(s)
	return msg
}

func (s *Stream) Reset() int {
	status := C.oruby_reset(s.ptr)
	runtime.KeepAlive(s)
	return int(status)
}

func (s *Stream) CopyDeflate() (*Stream, int) {
	var status C.int
	p := C.oruby_copy_deflate(s.ptr, &status)
	runtime.KeepAlive(s)
	if p == nil {
		return nil, int(status)
	}
	return wrapStream(p), OK
}

func (s *Stream) Step(in, out []byte, flush int) (used, written, status int) {
	step := C.oruby_run(s.ptr, bytePtr(in), size(in), bytePtr(out), size(out), C.int(flush))
	runtime.KeepAlive(s)
	return int(step.in), int(step.out), int(step.status)
}

func (s *Stream) Params(out []byte, level, strategy int) (written, status int) {
	step := C.oruby_params(s.ptr, bytePtr(out), size(out), C.int(level), C.int(strategy))
	runtime.KeepAlive(s)
	return int(step.out), int(step.status)
}

func (s *Stream) Sync(in []byte) (used, status int) {
	var n C.uint
	code := C.oruby_sync(s.ptr, bytePtr(in), size(in), &n)
	runtime.KeepAlive(s)
	return int(n), int(code)
}

func (s *Stream) SyncPoint() bool {
	status := C.oruby_sync_point(s.ptr)
	runtime.KeepAlive(s)
	return status == 1
}

func (s *Stream) SetDictionary(dict []byte) int {
	if uint64(len(dict)) > math.MaxUint32 {
		return StreamError
	}
	status := C.oruby_dictionary(s.ptr, bytePtr(dict), size(dict))
	runtime.KeepAlive(s)
	return int(status)
}

func (s *Stream) SetHeader(name, comment *string, mtime uint32, os int) int {
	var n, c *C.char
	if name != nil {
		n = C.CString(*name)
	}
	if comment != nil {
		c = C.CString(*comment)
	}
	status := C.oruby_set_header(s.ptr, n, c, C.uLong(mtime), C.int(os))
	runtime.KeepAlive(s)
	return int(status)
}

func bytePtr(b []byte) *C.Bytef {
	return (*C.Bytef)(unsafe.Pointer(unsafe.SliceData(b)))
}

func size(b []byte) C.uint {
	return C.uint(min(uint64(len(b)), math.MaxUint32))
}

func Version() string { return C.GoString(C.zlibVersion()) }

func Adler32(seed uint32, data []byte) uint32 {
	if len(data) == 0 {
		return seed
	}
	return uint32(C.adler32_z(C.uLong(seed), bytePtr(data), C.z_size_t(len(data))))
}

func CRC32(seed uint32, data []byte) uint32 {
	if len(data) == 0 {
		return seed
	}
	return uint32(C.crc32_z(C.uLong(seed), bytePtr(data), C.z_size_t(len(data))))
}

func Adler32Combine(first, second uint32, following int64) uint32 {
	return uint32(C.adler32_combine64(C.uLong(first), C.uLong(second), C.z_off64_t(following)))
}

func CRC32Combine(first, second uint32, following int64) uint32 {
	return uint32(C.crc32_combine64(C.uLong(first), C.uLong(second), C.z_off64_t(following)))
}

func CRCTable() []int {
	raw := unsafe.Slice(C.get_crc_table(), 256)
	table := make([]int, len(raw))
	for i, v := range raw {
		table[i] = int(v)
	}
	return table
}
