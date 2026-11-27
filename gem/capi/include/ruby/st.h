/* ORuby C API: MRI's ruby/st.h (the table of numbers, strings and any key).
   st_data_t, st_index_t and enum st_retval are in ruby/ruby.h. */
#ifndef ORUBY_CAPI_ST_H
#define ORUBY_CAPI_ST_H 1
#include "ruby/ruby.h"

#ifdef __cplusplus
extern "C" {
#endif

struct st_hash_type {
  int (*compare)(st_data_t, st_data_t); /* 0 when equal */
  st_index_t (*hash)(st_data_t);
};

typedef struct st_table_entry st_table_entry;
struct st_table_entry {
  st_index_t hash;
  st_data_t key;
  st_data_t record;
  st_table_entry *next;       /* the bin's chain */
  st_table_entry *fore, *back; /* insertion order */
};

typedef struct st_table st_table;
struct st_table {
  const struct st_hash_type *type;
  st_index_t num_bins;
  unsigned int entries_packed : 1;
  st_index_t num_entries;
  st_table_entry **bins;
  st_table_entry *head, *tail;
};

#define st_is_member(table, key) (st_lookup((table), (key), (st_data_t *)0))

st_table *st_init_table(const struct st_hash_type *);
st_table *st_init_table_with_size(const struct st_hash_type *, st_index_t);
st_table *st_init_numtable(void);
st_table *st_init_numtable_with_size(st_index_t);
st_table *st_init_strtable(void);
st_table *st_init_strtable_with_size(st_index_t);
st_table *st_init_strcasetable(void);
st_table *st_init_strcasetable_with_size(st_index_t);
int st_delete(st_table *, st_data_t *, st_data_t *);
int st_insert(st_table *, st_data_t, st_data_t);
int st_insert2(st_table *, st_data_t, st_data_t, st_data_t (*)(st_data_t));
int st_lookup(st_table *, st_data_t, st_data_t *);
int st_get_key(st_table *, st_data_t, st_data_t *);
void st_add_direct(st_table *, st_data_t, st_data_t);
void st_free_table(st_table *);
void st_clear(st_table *);
st_table *st_copy(st_table *);
int st_numcmp(st_data_t, st_data_t);
st_index_t st_numhash(st_data_t);
int st_foreach(st_table *, int (*)(st_data_t, st_data_t, st_data_t), st_data_t);

#ifdef __cplusplus
}
inline int st_foreach(st_table *t, int (*f)(...), st_data_t arg) {
  return st_foreach(t, reinterpret_cast<int (*)(st_data_t, st_data_t, st_data_t)>(f), arg);
}
#endif

#endif
