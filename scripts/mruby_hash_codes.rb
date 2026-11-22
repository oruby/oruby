# Keep full hash codes for tables that send #hash to their keys.
# The compact entry layout and ordinary small tables remain the default.
module ORubyHashCodes
  def self.patch(source)
    edit = ->(before, after) { ORubyVMCompat.replace(source, before, after) }

    edit.call("  uint32_t initial_pos;", "  uint32_t initial_pos;\n  uint64_t code;\n  mrb_bool compare_codes;")
    edit.call("#define h_ar_on(h) h_ht_off(h)", <<~'C'.chomp)
      #define ORUBY_HASH_CODES (1u << (MRB_HASH_SIZE_FLAGS_SHIFT + 4))
      #define h_has_codes(h) (((h)->flags & ORUBY_HASH_CODES) != 0)
      #define h_ar_on(h) do { h_ht_off(h); (h)->flags &= ~ORUBY_HASH_CODES; } while (0)
    C
    edit.call("#define H_CHECK_MODIFIED_FLAGS_MASK  (MRB_HASH_HT | MRB_HASH_IB_BIT_MASK | MRB_HASH_AR_EA_CAPA_MASK)",
      "#define H_CHECK_MODIFIED_FLAGS_MASK  (MRB_HASH_HT | MRB_HASH_IB_BIT_MASK | MRB_HASH_AR_EA_CAPA_MASK | ORUBY_HASH_CODES | MRB_HASH_COMPARE_BY_IDENTITY)")
    edit.call("static uint32_t\nkey_hash_code", <<~'C'.chomp)
      static inline mrb_bool
      ruby_hash_key(mrb_value key)
      {
        const uint64_t builtin = (UINT64_C(1) << MRB_TT_TRUE) |
          (UINT64_C(1) << MRB_TT_FALSE) | (UINT64_C(1) << MRB_TT_SYMBOL) |
          (UINT64_C(1) << MRB_TT_INTEGER) | (UINT64_C(1) << MRB_TT_STRING) |
          (UINT64_C(1) << MRB_TT_FLOAT);
        mrb_static_assert(MRB_TT_MAXDEFINE <= 64, "hash key types fit in a bitmask");
        return !(builtin & (UINT64_C(1) << mrb_type(key)));
      }

      static uint64_t
      mix_ruby_hash(mrb_int value)
      {
        uint64_t code = (uint64_t)value;
        return code ^ (code >> 31);
      }

      static uint64_t
      ruby_hash_code(mrb_state *mrb, mrb_value key)
      {
        mrb_value result = mrb_funcall_argv(mrb, key, MRB_SYM(hash), 0, NULL);
      #ifndef MRB_NO_FLOAT
        if (mrb_float_p(result)) result = mrb_ensure_integer_type(mrb, result);
      #endif
        if (!mrb_integer_p(result) && !mrb_bigint_p(result)) {
          result = mrb_type_convert(mrb, result, MRB_TT_INTEGER, MRB_SYM(to_int));
        }
      #ifdef MRB_USE_BIGINT
        if (mrb_bigint_p(result)) result = mrb_bint_hash(mrb, result);
      #endif
        return mix_ruby_hash(mrb_integer(result));
      }

      /* Builtin-only tables use their original cheap codes. A comparison
         with a custom key uses the public default #hash, including salt. */
      static uint64_t
      builtin_ruby_hash_code(mrb_state *mrb, mrb_value key)
      {
        mrb_int hash;
        switch (mrb_type(key)) {
        case MRB_TT_STRING:
          hash = (mrb_int)mrb_str_hash(mrb, key);
          break;
        case MRB_TT_INTEGER: {
          mrb_int value = mrb_integer(key);
          hash = (mrb_int)mrb_byte_hash((const uint8_t *)&value, sizeof(value));
          break;
        }
      #ifndef MRB_NO_FLOAT
        case MRB_TT_FLOAT:
          hash = (mrb_int)float_hash_code(mrb_float(key));
          break;
      #endif
        default:
          hash = mrb_obj_id(key);
          break;
        }
        if (mrb->kernel_module) {
          mrb_value salt = mrb_iv_get(mrb, mrb_obj_value(mrb->kernel_module),
                                      mrb_intern_lit(mrb, "@__hash_salt"));
          if (mrb_integer_p(salt)) hash ^= mrb_integer(salt);
        }
        return mix_ruby_hash(hash);
      }

      static uint64_t
      key_hash_code
    C
    edit.call("  mrb_value hash_code_obj;\n", "")
    edit.call(<<~'C'.chomp.gsub(/^/, "  "), "  default:\n    return ruby_hash_code(mrb, key);")
      default:
        hash_code_obj = mrb_funcall_argv(mrb, key, MRB_SYM(hash), 0, NULL);
        hash_code = U32(tt) ^ U32(mrb_integer(hash_code_obj));
        break;
    C
    edit.call("static uint32_t\nobj_hash_code", "static uint64_t\nobj_hash_code")
    edit.call("  uint32_t hash_code = 0;", "  uint64_t hash_code = 0;")
    edit.call("    hash_code = key_hash_code(mrb, key, h);",
      "    hash_code = h_has_codes(h) && !h_identity_p(h) && !ruby_hash_key(key) ?\n" \
      "      builtin_ruby_hash_code(mrb, key) : key_hash_code(mrb, key, h);")

    edit.call(<<~'C'.chomp.gsub(/^/, "  "), <<~'C'.chomp.gsub(/^/, "  "))
      EA_EACH(ea, ea_capa, size, entry) {
        if (entry_key_eql(mrb, h, entry, key)) return entry;
      }
      return NULL;
    C
      /* A compact table holds only builtin keys. A custom lookup still
         calls #hash once, even when none of those keys share its code. */
      if (mrb_unlikely(size && ruby_hash_key(key))) {
        uint64_t code = obj_hash_code(mrb, key, h);
        EA_EACH(ea, ea_capa, size, entry) {
          if (builtin_ruby_hash_code(mrb, entry->key) == code &&
              entry_key_eql(mrb, h, entry, key)) return entry;
        }
        return NULL;
      }
      EA_EACH(ea, ea_capa, size, entry) {
        if (entry_key_eql(mrb, h, entry, key)) return entry;
      }
      return NULL;
    C

    edit.call("static void\nea_compress(hash_entry *ea, uint32_t n_used)",
      "static void\nea_compress(hash_entry *ea, uint32_t n_used, uint64_t *codes)")
    edit.call("    if (r_entry != w_entry) *w_entry = *r_entry;", <<~'C'.chomp.gsub(/^/, "    "))
      if (r_entry != w_entry) {
        *w_entry = *r_entry;
        if (codes) codes[w_entry - ea] = codes[r_entry - ea];
      }
    C
    edit.call("  ea_compress(ar_ea(h), ar_ea_n_used(h));", "  ea_compress(ar_ea(h), ar_ea_n_used(h), NULL);")
    edit.call("  ea_compress(ea, ht_ea_n_used(h));", "  ea_compress(ea, ht_ea_n_used(h), NULL);")
    # Both growth and tombstone compaction move a coded table's entries.
    edit.call("if (size != ht_ea_n_used(h)) ea_compress(ht_ea(h), ht_ea_n_used(h));",
      "if (size != ht_ea_n_used(h)) ea_compress(ht_ea(h), ht_ea_n_used(h), ht_codes(h));")
    edit.call("        ea_compress(ht_ea(h), ht_ea_n_used(h));",
      "        ea_compress(ht_ea(h), ht_ea_n_used(h), ht_codes(h));")

    edit.call("static index_buckets_iter\nib_it_init(mrb_state *mrb, struct RHash *h, mrb_value key)", <<~'C'.chomp)
      static inline uint64_t *
      ht_codes(const struct RHash *h)
      {
        return h_has_codes(h) && ht_ea(h) ? (uint64_t *)(ht_ea(h) + ht_ea_capa(h)) : NULL;
      }

      static index_buckets_iter
      ib_it_init_code(struct RHash *h, uint64_t code, mrb_bool compare_codes)
    C
    edit.call("  it.initial_pos = ib_it_pos_for(&it, obj_hash_code(mrb, key, h));",
      "  it.code = code;\n  it.compare_codes = compare_codes;\n  it.initial_pos = ib_it_pos_for(&it, U32(code));")
    edit.call("static void\nib_it_next", <<~'C'.chomp)
      static index_buckets_iter
      ib_it_init(mrb_state *mrb, struct RHash *h, mrb_value key)
      {
        uint64_t code = obj_hash_code(mrb, key, h);
        return ib_it_init_code(h, code, h_has_codes(h) && !h_identity_p(h));
      }

      static inline mrb_bool
      ib_it_matches(mrb_state *mrb, index_buckets_iter *it, mrb_value key)
      {
        hash_entry *entry = ib_it_entry(it);
        if (it->compare_codes) {
          uint64_t *codes = ht_codes(it->h);
          if (codes[it->ea_index] != it->code) return FALSE;
        }
        return entry_holds_key_p(mrb, it->h, entry, key);
      }

      static void
      ib_it_next
    C
    edit.call("entry_holds_key_p(mrb, it->h, ib_it_entry(it), key)", "ib_it_matches(mrb, it, key)")
    # ht_set and native ht_rehash use the same comparison.
    edit.call("\n      if (!entry_holds_key_p(mrb, h, ib_it_entry(it), key)) continue;",
      "\n      if (!ib_it_matches(mrb, it, key)) continue;")
    edit.call("\n        if (!entry_holds_key_p(mrb, h, ib_it_entry(it), key)) continue;",
      "\n        if (!ib_it_matches(mrb, it, key)) continue;")

    edit.call("  EA_EACH_USED(ea, ht_ea_n_used(h), entry) {\n    IB_CYCLE_BY_KEY(mrb, h, entry->key, it) {", <<~'C'.chomp.gsub(/^/, "  "))
      if (!ht_ea_n_used(h)) return;
      uint64_t *codes = ht_codes(h);
      EA_EACH_USED(ea, ht_ea_n_used(h), entry) {
        if (entry_deleted_p(entry)) continue;
        uint64_t code = codes ? codes[entry - ea] : key_hash_code(mrb, entry->key, h);
        index_buckets_iter it[1] = { ib_it_init_code(h, code, FALSE) };
        for (;;) {
          ib_it_next(it);
    C
    edit.call("    IB_CYCLE_BY_KEY(mrb, h, entry->key, it) {\n      if (ib_it_get(it)", <<~'C'.chomp.gsub(/^/, "    "))
      uint64_t *codes = ht_codes(h);
      uint64_t code = codes ? codes[entry - ea] : key_hash_code(mrb, entry->key, h);
      index_buckets_iter it[1] = { ib_it_init_code(h, code, FALSE) };
      for (;;) {
        ib_it_next(it);
        if (ib_it_get(it)
    C

    edit.call("static void\nht_adjust_ea", <<~'C'.chomp)
      /* Put the sidecar after the entries in the same allocation. For a
         resize, allocate first so failure leaves entries and codes intact. */
      static hash_entry *
      ht_resize_coded(mrb_state *mrb, struct RHash *h, uint32_t capa)
      {
        uint32_t old_capa = ht_ea_capa(h);
        uint32_t copy = lesser(capa, old_capa);
        uint64_t *old_codes = ht_codes(h);
        hash_entry *old = ht_ea(h);
      #if SIZE_MAX <= UINT32_MAX
        if (capa > SIZE_MAX / (sizeof(hash_entry) + sizeof(uint64_t))) {
          mrb_raise(mrb, E_ARGUMENT_ERROR, "hash too big");
        }
      #endif
        hash_entry *ea = (hash_entry *)mrb_malloc(mrb, (size_t)capa * (sizeof(hash_entry) + sizeof(uint64_t)));
        uint64_t *codes = (uint64_t *)(ea + capa);
        if (copy) memcpy(ea, old, (size_t)copy * sizeof(hash_entry));
        if (old_codes) {
          memcpy(codes, old_codes, (size_t)copy * sizeof(uint64_t));
        }
        else {
          /* Until this point the table only held builtin keys. */
          for (uint32_t i = 0; i < copy; ++i) {
            codes[i] = entry_deleted_p(&ea[i]) ? 0 : builtin_ruby_hash_code(mrb, ea[i].key);
          }
        }
        for (uint32_t i = copy; i < capa; ++i) {
          entry_delete(&ea[i]);
          codes[i] = 0;
        }
        mrb_free(mrb, old);
        return ea;
      }

      static void
      ht_enable_codes(mrb_state *mrb, struct RHash *h)
      {
        uint32_t capa = ht_ea_capa(h);
        if (capa) ht_set_ea(h, ht_resize_coded(mrb, h, capa));
        h->flags |= ORUBY_HASH_CODES;
        uint32_t bit = ib_bit(h);
        ib_init(mrb, h, bit, ib_byte_size_for(bit));
      }

      static void
      ht_adjust_ea
    C
    edit.call("  hash_entry *ea = ea_adjust(mrb, ht_ea(h), &ea_capa, max_ea_capa);",
      "  hash_entry *ea;\n  if (h_has_codes(h)) {\n    ea_capa = ea_next_capa_for(size, max_ea_capa);\n" \
      "    ea = ht_resize_coded(mrb, h, ea_capa);\n  }\n" \
      "  else ea = ea_adjust(mrb, ht_ea(h), &ea_capa, max_ea_capa);")
    edit.call("static void\nht_set(mrb_state *mrb, struct RHash *h, mrb_value key, mrb_value val)", <<~'C'.chomp)
      static void
      ht_rebuild_coded_index(mrb_state *mrb, struct RHash *h, uint32_t bit)
      {
        uint32_t size = ht_size(h);
        uint32_t capacity = ht_ea_capa(h);
        hash_entry *entries = ht_ea(h);
        size_t bucket_bytes = ib_byte_size_for(bit);
        hash_table *old = h_ht(h);
        hash_table *table = (hash_table *)mrb_malloc(mrb, sizeof(hash_table) + bucket_bytes);
        /* Allocation has succeeded. Moving entries and rebuilding buckets
           with their saved codes needs neither Ruby calls nor allocation. */
        if (size != ht_ea_n_used(h)) ea_compress(entries, ht_ea_n_used(h), ht_codes(h));
        h_set_ht(h, table);
        ht_set_ea(h, entries);
        ht_set_ea_capa(h, capacity);
        ht_set_ea_n_used(h, size);
        ib_init(mrb, h, bit, bucket_bytes);
        mrb_free(mrb, old);
      }

      static void
      ht_set(mrb_state *mrb, struct RHash *h, mrb_value key, mrb_value val)
    C
    edit.call("  if (ib_upper_bound_for(ib_capa) <= size) {",
      <<~'C'.chomp.gsub(/^/, "  "))
      if (h_has_codes(h)) {
        uint32_t used = ht_ea_n_used(h);
        uint32_t capacity = ht_ea_capa(h);
        if (ib_upper_bound_for(ib_capa) <= size) {
          ht_rebuild_coded_index(mrb, h, ib_bit_width + 1);
        }
        else if (size != used &&
                 (used >= ib_capa - EA_N_RESERVED_INDICES ||
                  (used == capacity && ea_next_capa_for(size, EA_MAX_CAPA) <= capacity))) {
          ht_rebuild_coded_index(mrb, h, ib_bit_width);
        }
      }
      else if (ib_upper_bound_for(ib_capa) <= size) {
    C
    edit.call("  IB_CYCLE_BY_KEY(mrb, h, key, it) {\n    if (ib_it_active_p(it)) {", <<~'C'.chomp.gsub(/^/, "  "))
      if (!h_has_codes(h) && !h_identity_p(h) && ruby_hash_key(key)) ht_enable_codes(mrb, h);
      IB_CYCLE_BY_KEY(mrb, h, key, it) {
        if (ib_it_active_p(it)) {
    C
    edit.call("      if (size <= AR_MAX_SIZE) {\n        ht_set_as_ar", "      if (size <= AR_MAX_SIZE && !h_has_codes(h) &&\n          (h_identity_p(h) || !ruby_hash_key(key))) {\n        ht_set_as_ar")
    edit.call("      ea_set(ht_ea(h), ea_n_used, key, val);",
      "      ea_set(ht_ea(h), ea_n_used, key, val);\n      if (h_has_codes(h)) ht_codes(h)[ea_n_used] = it->code;")
    edit.call("static mrb_bool\nht_get(mrb_state *mrb, struct RHash *h, mrb_value key, mrb_value *valp)\n{", <<~'C'.chomp)
      static inline mrb_bool
      ht_needs_mixed_lookup(struct RHash *h, mrb_value key)
      {
        return !(h->flags & (ORUBY_HASH_CODES | MRB_HASH_COMPARE_BY_IDENTITY)) && ruby_hash_key(key);
      }

      static mrb_bool
      ht_get(mrb_state *mrb, struct RHash *h, mrb_value key, mrb_value *valp)
      {
        if (mrb_unlikely(!ht_size(h))) return FALSE;
        if (mrb_unlikely(ht_needs_mixed_lookup(h, key))) {
          /* A read does not change the representation of a builtin table.
             Its bucket codes are different from the published hash codes. */
          hash_entry *entry = ea_get_by_key(mrb, ht_ea(h), ht_ea_capa(h), ht_size(h), key, h);
          if (!entry) return FALSE;
          *valp = entry->val;
          return TRUE;
        }
    C
    edit.call("static mrb_bool\nht_delete(mrb_state *mrb, struct RHash *h, mrb_value key, mrb_value *valp)\n{", <<~'C'.chomp)
      static mrb_bool
      ht_delete(mrb_state *mrb, struct RHash *h, mrb_value key, mrb_value *valp)
      {
        if (mrb_unlikely(!ht_size(h))) return FALSE;
        if (mrb_unlikely(ht_needs_mixed_lookup(h, key))) {
          hash_entry *entry = ea_get_by_key(mrb, ht_ea(h), ht_ea_capa(h), ht_size(h), key, h);
          if (!entry) return FALSE;
          uint32_t index = U32(entry - ht_ea(h));
          index_buckets_iter it[1] = { ib_it_init_code(h, key_hash_code(mrb, entry->key, h), FALSE) };
          for (;;) {
            ib_it_next(it);
            if (ib_it_empty_p(it)) return FALSE;
            if (ib_it_get(it) == index) break;
          }
          *valp = entry->val;
          ib_it_delete(it);
          entry_delete(entry);
          ht_dec_size(h);
          return TRUE;
        }
    C
    edit.call("  (h_ar_p(h) ? ar_set : ht_set)(mrb, h, key, val);", <<~'C'.chomp.gsub(/^/, "  "))
      if (h_ar_p(h) && mrb_unlikely(ruby_hash_key(key)) && !h_identity_p(h)) {
        uint32_t size = ar_size(h);
        hash_entry *ea = ar_ea(h);
        if (size != ar_ea_n_used(h)) ea_compress(ea, ar_ea_n_used(h), NULL);
        uint32_t bit = ib_bit_for(size + 1);
        ht_init(mrb, h, size, ea, ar_ea_capa(h), NULL, bit < IB_INIT_BIT ? IB_INIT_BIT : bit);
      }
      (h_ar_p(h) ? ar_set : ht_set)(mrb, h, key, val);
    C

    edit.call("static void\nh_rehash", <<~'C'.chomp)
      static void h_replace(mrb_state *, struct RHash *, struct RHash *, mrb_bool);

      /* Only rehash calls user hash again. Build the replacement separately
         so an exception leaves the previous codes and index usable. */
      static void
      ht_rehash_coded(mrb_state *mrb, struct RHash *h, mrb_bool identity)
      {
        mrb_value replacement = mrb_hash_new_capa(mrb, h_size(h));
        struct RHash *new_hash = mrb_hash_ptr(replacement);
        if (identity) h_identity_on(new_hash);
        int ai = mrb_gc_arena_save(mrb);
        H_EACH(h, entry) {
          mrb_value key = entry->key;
          H_CHECK_MODIFIED(mrb, h) {
            mrb_hash_set(mrb, replacement, key, entry->val);
          }
          entry_check_vacated(mrb, entry, key);
          mrb_gc_arena_restore(mrb, ai);
        }
        h_replace(mrb, h, new_hash, TRUE);
        if (identity) h_identity_on(h);
      }

      static void
      h_rehash
    C
    edit.call("  (h_size(h) == 0 ? h_clear : h_ar_p(h) ? ar_rehash : ht_rehash)(mrb, h);",
      "  if (h_has_codes(h) && h_size(h)) ht_rehash_coded(mrb, h, h_identity_p(h));\n" \
      "  else (h_size(h) == 0 ? h_clear : h_ar_p(h) ? ar_rehash : ht_rehash)(mrb, h);")
    edit.call("  h_identity_on(h);\n  h_rehash(mrb, h);",
      "  if (h_has_codes(h) && h_size(h)) ht_rehash_coded(mrb, h, TRUE);\n" \
      "  else {\n    h_identity_on(h);\n    h_rehash(mrb, h);\n  }")
    edit.call("h_replace(mrb_state *mrb, struct RHash *h, struct RHash *orig_h)",
      "h_replace(mrb_state *mrb, struct RHash *h, struct RHash *orig_h, mrb_bool take_table)")
    edit.call("  h_replace(mrb, h, orig_h);", "  h_replace(mrb, h, orig_h, FALSE);")
    edit.call("ea_dup(mrb_state *mrb, const hash_entry *ea, uint32_t capa)",
      "ea_dup(mrb_state *mrb, const hash_entry *ea, uint32_t capa, mrb_bool coded)")
    edit.call("  size_t byte_size = sizeof(hash_entry) * capa;",
      "  size_t byte_size = (size_t)capa * (sizeof(hash_entry) + (coded ? sizeof(uint64_t) : 0));")
    edit.call("ea_dup(mrb, ar_ea(orig_h), ea_capa)",
      "take_table ? ar_ea(orig_h) : ea_dup(mrb, ar_ea(orig_h), ea_capa, FALSE)")
    edit.call("ea_dup(mrb, ht_ea(orig_h), ea_capa)",
      "take_table ? ht_ea(orig_h) : ea_dup(mrb, ht_ea(orig_h), ea_capa, h_has_codes(orig_h))")
    edit.call("    hash_table *ht = ht_dup(mrb, orig_h);",
      "    hash_table *ht = take_table ? h_ht(orig_h) : ht_dup(mrb, orig_h);")
    edit.call("    ib_set_bit(h, ib_bit(orig_h));", <<~'C'.chomp.gsub(/^/, "    "))
      ib_set_bit(h, ib_bit(orig_h));
      h->flags = (h->flags & ~ORUBY_HASH_CODES) | (orig_h->flags & ORUBY_HASH_CODES);
    C
    edit.call("  /* the entries came in by a copy, past the barrier on each store */",
      "  if (take_table) h_init(orig_h);\n  /* The entries arrived without passing the barrier on each store. */")
    edit.call("                      (ht_ea_capa(h) * sizeof(hash_entry) +",
      "                      (ht_ea_capa(h) * (sizeof(hash_entry) + (h_has_codes(h) ? sizeof(uint64_t) : 0)) +")
  end
end
