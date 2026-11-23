#ifndef GO_ORB_H
#define GO_ORB_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/*
 * ORB (ORuby Bundle) Binary Format Specification (Version 1)
 *
 * File Layout:
 *   1. Fixed Header (32 bytes, little-endian)
 *   2. Data Arena (4-byte aligned payloads: RITE .mrb binaries, .rb, assets)
 *   3. Index Table (EntryCount * 32-byte orb_entry_t records, sorted by path)
 *   4. String Table (UTF-8 virtual paths packed contiguously)
 */

#define ORB_MAGIC_0 'O'
#define ORB_MAGIC_1 'R'
#define ORB_MAGIC_2 'B'
#define ORB_MAGIC_3 '\0'

#define ORB_VERSION       1U
#define ORB_HEADER_SIZE   32U
#define ORB_ENTRY_SIZE    32U
#define ORB_ALIGNMENT     4U
#define ORB_NO_ENTRYPOINT 0xFFFFFFFFU

/* Entry Kind (orb_entry_t.kind) */
typedef enum {
  ORB_KIND_MRB   = 1, /* Precompiled mruby RiteVM bytecode (.mrb payload) */
  ORB_KIND_RUBY  = 2, /* Raw Ruby source (.rb payload) */
  ORB_KIND_ASSET = 3, /* Static asset/data file (.yml, .erb, .json, etc.) */
  ORB_KIND_DIR   = 4  /* Virtual directory entry */
} orb_entry_kind_t;

/* Entry Flags (orb_entry_t.flags bitmask) */
typedef enum {
  ORB_FLAG_ENTRYPOINT = 0x01, /* Default script executed when bundle runs */
  ORB_FLAG_LOAD_PATH  = 0x02, /* Directory added to $LOAD_PATH on mount */
  ORB_FLAG_EXECUTABLE = 0x04  /* Executable script file */
} orb_entry_flags_t;

/* Header Flags (orb_header_t.flags bitmask) */
typedef enum {
  ORB_HDR_DEBUG_INFO = 0x01 /* Compiled .mrb entries contain debug line info */
} orb_header_flags_t;

/* 32-byte Fixed File Header (little-endian on disk) */
typedef struct orb_header_t {
  uint8_t  magic[4];        /* 0..3:   "ORB\0" */
  uint16_t version;         /* 4..5:   Format version (1) */
  uint16_t flags;           /* 6..7:   orb_header_flags_t bitmask */
  uint32_t entry_count;     /* 8..11:  Number of 32-byte index entries */
  uint32_t entrypoint_idx;  /* 12..15: Index of entrypoint, or ORB_NO_ENTRYPOINT */
  uint64_t index_offset;    /* 16..23: Byte offset of Index Table */
  uint64_t strings_offset;  /* 24..31: Byte offset of String Table */
} orb_header_t;

/* 32-byte Fixed Index Table Entry (little-endian on disk) */
typedef struct orb_entry_t {
  uint32_t path_offset; /* 0..3:   Offset of virtual path in String Table */
  uint16_t path_len;    /* 4..5:   Byte length of virtual path */
  uint8_t  kind;        /* 6:      orb_entry_kind_t */
  uint8_t  flags;       /* 7:      orb_entry_flags_t bitmask */
  uint32_t hash32;      /* 8..11:  FNV-1a 32-bit hash of payload bytes */
  uint32_t mode;        /* 12..15: Unix file mode / permissions */
  uint64_t data_offset; /* 16..23: 4-byte-aligned offset into Data Arena */
  uint64_t data_size;   /* 24..31: Byte length of payload */
} orb_entry_t;

/* Compile-time size checks */
typedef char _orb_static_assert_header[(sizeof(orb_header_t) == 32) ? 1 : -1];
typedef char _orb_static_assert_entry[(sizeof(orb_entry_t) == 32) ? 1 : -1];

static inline int orb_is_valid_header(const void *buf, size_t len) {
  const uint8_t *b = (const uint8_t *)buf;
  if (!b || len < ORB_HEADER_SIZE) {
    return 0;
  }
  if (b[0] != ORB_MAGIC_0 || b[1] != ORB_MAGIC_1 ||
      b[2] != ORB_MAGIC_2 || b[3] != ORB_MAGIC_3) {
    return 0;
  }
  uint16_t ver = (uint16_t)b[4] | ((uint16_t)b[5] << 8);
  return ver == ORB_VERSION ? 1 : 0;
}

#ifdef __cplusplus
}
#endif

#endif /* GO_ORB_H */
