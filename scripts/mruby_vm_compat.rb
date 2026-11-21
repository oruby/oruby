# Build the compatibility changes from the vendored sources without changing
# the submodule. Fail the build when the owning upstream code has drifted.
module ORubyVMCompat
  def self.replace(source, before, after)
    index = source.index(before)
    unless index && !source.index(before, index + before.bytesize)
      raise "ORuby VM compatibility patch needs review near #{before.lines.first.strip}"
    end
    source[index, before.bytesize] = after
  end

  def self.vm(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include \"oruby-vm-compat.h\"")
    replace(source, "uint32_t h = mrb_int_hash_func(mrb, ((intptr_t)irep) ^ sym) & (MRB_CONST_CACHE_SIZE-1);",
      "/* One irep can be closed over different lexical class scopes. */\n" \
      "      const mrb_irep *scope_key = (const mrb_irep*)ci->proc;\n" \
      "      uint32_t h = mrb_int_hash_func(mrb, ((intptr_t)scope_key) ^ sym) & (MRB_CONST_CACHE_SIZE-1);")
    replace(source, "cc->irep == irep && cc->sym == sym", "cc->irep == scope_key && cc->sym == sym")
    replace(source, "cc->irep = irep;", "cc->irep = scope_key;")

    replace(source, <<~'C'.chomp.gsub(/^/, "      "), <<~'C'.chomp.gsub(/^/, "      "))
      c = mrb_vm_define_class(mrb, base, super, id);
    C
      if (qualified && (mrb_class_p(base) || mrb_module_p(base) || mrb_sclass_p(base)) &&
          (mrb_const_flags(mrb, mrb_class_ptr(base), id) & MRB_CONST_PRIVATE)) {
        if (!mrb_nil_p(super) && !mrb_class_p(super)) {
          mrb_raisef(mrb, E_TYPE_ERROR, "superclass must be a Class (%!v given)", super);
        }
        mrb_value value = mrb_vm_const_get_path(mrb, base, id);
        if (!mrb_class_p(value)) mrb_raisef(mrb, E_TYPE_ERROR, "%!v is not a class", value);
        c = mrb_class_ptr(value);
        if (!mrb_nil_p(super) && mrb_class_real(c->super) != mrb_class_ptr(super)) {
          mrb_raisef(mrb, E_TYPE_ERROR, "superclass mismatch for %v", value);
        }
      }
      else {
        c = mrb_vm_define_class(mrb, base, super, id);
      }
    C
    replace(source, "mrb_value super = regs[a+1];\n\n      if (mrb_nil_p(base))",
      "mrb_value super = regs[a+1];\n      mrb_bool qualified = !mrb_nil_p(base);\n\n      if (!qualified)")

    replace(source, <<~'C'.chomp.gsub(/^/, "      "), <<~'C'.chomp.gsub(/^/, "      "))
      cls = mrb_vm_define_module(mrb, base, id);
    C
      if (qualified && (mrb_class_p(base) || mrb_module_p(base) || mrb_sclass_p(base)) &&
          (mrb_const_flags(mrb, mrb_class_ptr(base), id) & MRB_CONST_PRIVATE)) {
        mrb_value value = mrb_vm_const_get_path(mrb, base, id);
        if (!mrb_module_p(value)) mrb_raisef(mrb, E_TYPE_ERROR, "%!v is not a module", value);
        cls = mrb_class_ptr(value);
      }
      else {
        cls = mrb_vm_define_module(mrb, base, id);
      }
    C
    replace(source, "mrb_value base = regs[a];\n\n      if (mrb_nil_p(base))",
      "mrb_value base = regs[a];\n      mrb_bool qualified = !mrb_nil_p(base);\n\n      if (!qualified)")

    replace(source, "static int\nvm_op_enter(mrb_state *mrb, uint32_t a)", <<~'C'.chomp)
      /* Only a positional splat may forward a marked keyword hash. */
      static void
      oruby_forward_keywords(mrb_state *mrb, mrb_callinfo *ci)
      {
        mrb_value args = regs[1];
        if (!(mrb_ary_ptr(args)->flags & ORUBY_ARRAY_SPLAT)) return;
        mrb_int len = RARRAY_LEN(args);
        if (len == 0) return;
        mrb_value last = RARRAY_PTR(args)[len - 1];
        if (!mrb_hash_p(last) || !mrb_test(mrb_iv_get(mrb, last, MRB_IVSYM(__ruby2_keywords)))) return;

        stack_extend(mrb, 4);
        regs[3] = ci->blk ? mrb_obj_value(ci->blk) : mrb_nil_value();
        regs[2] = mrb_hash_dup(mrb, last);
        regs[1] = mrb_ary_new_from_values(mrb, len - 1, RARRAY_PTR(args));
        ci->kw = TRUE;
      }

      static int
      vm_op_enter(mrb_state *mrb, uint32_t a)
    C
    replace(source, '    ARY_SET_LEN(rest, m1+len+m2);',
      "    ARY_SET_LEN(rest, m1+len+m2);\n    rest->flags |= ORUBY_ARRAY_SPLAT;")
    replace(source, <<~'C'.chomp.gsub(/^(?=.)/, "    "), <<~'C'.chomp.gsub(/^(?=.)/, "    "))
        }
        mrb_gc_arena_restore(mrb, ai);
        NEXT;
      }

      CASE(OP_ARYPUSH, BB) {
    C
        }
        mrb_ary_ptr(regs[a])->flags |= ORUBY_ARRAY_SPLAT;
        mrb_gc_arena_restore(mrb, ai);
        NEXT;
      }

      CASE(OP_ARYPUSH, BB) {
    C
    replace(source, '    if (!mrb_nil_p(kdict) && mrb_hash_p(kdict) && mrb_hash_size(mrb, kdict) > 0) {', <<~'C'.chomp.gsub(/^/, "    "))
      if (!mrb_nil_p(kdict) && mrb_hash_p(kdict) && mrb_hash_size(mrb, kdict) > 0) {
        if (irep->flags & ORUBY_IREP_RUBY2_KEYWORDS) {
          mrb_iv_set(mrb, kdict, MRB_IVSYM(__ruby2_keywords), mrb_true_value());
        }
    C
    replace(source, '      ci = cipush(mrb, a, CINFO_DIRECT, NULL, NULL, BLK_PTR(blk), 0, c);',
      "      ci = cipush(mrb, a, CINFO_DIRECT, NULL, NULL, BLK_PTR(blk), 0, c);\n" \
      "      if (c == CALL_MAXARGS) oruby_forward_keywords(mrb, ci);")
    replace(source, "      int r = vm_call_proc(mrb, p, b+1, &irep, ai, &pret);",
      "      mrb_int nargs = b+1;\n" \
      "      if (b == CALL_MAXARGS) {\n" \
      "        oruby_forward_keywords(mrb, ci);\n" \
      "        if (ci->kw) nargs = mrb_ci_bidx(ci);\n" \
      "      }\n" \
      "      int r = vm_call_proc(mrb, p, nargs, &irep, ai, &pret);")

    replace(source, '      mrb_value ret = mrb_obj_as_string_nomethod(mrb, regs[a+1]);', <<~'C'.chomp.gsub(/^/, "      "))
      mrb_value ret;
      #ifdef MRB_USE_REFINEMENTS
      if (mrb->refscopes_len && !mrb_string_p(regs[a+1]) && mrb_refined_mid_p(mrb, MRB_SYM(to_s)) &&
          mrb_vm_refinements(mrb, ci)) {
        /* The normal scalar conversion would skip the refined #to_s. */
        ret = mrb_nil_value();
      }
      else
      #endif
      ret = mrb_obj_as_string_nomethod(mrb, regs[a+1]);
    C
  end

  def self.const_names(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include <prism/internal/encoding.h>")
    replace(source, '  return len > 0 && ISUPPER(name[0]) && mrb_ident_p(name+1, len-1);', <<~'C'.chomp.gsub(/^/, "  "))
      if (len <= 0) return FALSE;
      if (ISASCII(name[0])) return ISUPPER(name[0]) && mrb_ident_p(name+1, len-1);
      return pm_encoding_utf_8_isupper_char((const uint8_t*)name, len) &&
             mrb_ident_p(name+1, len-1);
    C
  end

  def self.refinement(source)
    replace(source, '  struct RProc *np = MRB_OBJ_ALLOC(mrb, MRB_TT_PROC, mrb->proc_class);',
      '  struct RProc *np = MRB_OBJ_ALLOC(mrb, MRB_TT_PROC, mrb_obj_class(mrb, self));')
    replace(source, '  if (MRB_PROC_CFUNC_P(p) || MRB_PROC_ALIAS_P(p) || (MRB_PROC_SCOPE_P(p) && MRB_PROC_STRICT_P(p))) {',
      "  if (MRB_PROC_CFUNC_P(p) || MRB_PROC_ALIAS_P(p) || (MRB_PROC_SCOPE_P(p) && MRB_PROC_STRICT_P(p)) ||\n" \
      "      mrb_iv_defined(mrb, self, MRB_IVSYM(__oruby_method)) ||\n" \
      "      mrb_iv_defined(mrb, self, MRB_IVSYM(__oruby_symproc))) {")
  end

  def self.proc_identity(source)
    replace(source, '  else if (MRB_PROC_ENV(p1) != MRB_PROC_ENV(p2)) return FALSE;', <<~'C'.chomp.gsub(/^/, "  "))
      else if (MRB_PROC_ENV(p1) != MRB_PROC_ENV(p2)) return FALSE;
      #ifdef MRB_USE_REFINEMENTS
      if (MRB_PROC_REFSCOPE(p1) != MRB_PROC_REFSCOPE(p2) &&
          (mrb_proc_refined_p(mrb, p1) || mrb_proc_refined_p(mrb, p2))) return FALSE;
      #endif
    C
  end

  def self.gc(source)
    replace(source, "  gc->state = MRB_GC_STATE_SWEEP;", <<~'C'.chomp.gsub(/^/, "  "))
      /* Drop weak Proc cache keys before their storage can be recycled. */
      mrb_const_cache_clear(mrb);
      gc->state = MRB_GC_STATE_SWEEP;
    C
  end

  def self.weakmap(source)
    replace(source, "    mrb_bool eq = mrb_eql(mrb, k, key);", "    mrb_bool eq = mrb_eql(mrb, key, k);")
    replace(source, <<~'C'.chomp.gsub(/^/, "  "), <<~'C'.chomp.gsub(/^/, "  "))
      if (mrb_nil_p(blk)) mrb_raise(mrb, E_LOCALJUMP_ERROR, "no block given (yield)");
      struct wm *m = wm_get(mrb, self, FALSE);
      if (!m) return self;
    C
      struct wm *m = wm_get(mrb, self, FALSE);
      if (!m || !m->size) return self;
      if (mrb_nil_p(blk)) mrb_raise(mrb, E_LOCALJUMP_ERROR, "no block given (yield)");
    C
  end

  def self.method_lookup(source)
    replace(source, <<~'C'.chomp, <<~'C'.chomp)
      static const struct RProc *
      method_search_vm(mrb_state *mrb, struct RClass **cp, mrb_sym mid)
      {
        mrb_method_t m = mrb_method_search_vm(mrb, cp, mid);
    C
      static const struct RProc *
      method_search_scoped(mrb_state *mrb, struct RClass **cp, mrb_sym mid, mrb_bool lexical)
      {
        mrb_method_t m;
      #ifdef MRB_USE_REFINEMENTS
        struct RArray *scope = lexical && mrb->refscopes_len && mrb_refined_mid_p(mrb, mid) ?
          mrb_vm_caller_refinements(mrb) : NULL;
        if (scope) m = mrb_vm_find_method_in_scope(mrb, scope, *cp, cp, mid);
        else
      #endif
        m = mrb_method_search_vm(mrb, cp, mid);
    C
    replace(source, 'method_search_vm(mrb, &super, mrb_symbol(name))',
      'method_search_scoped(mrb, &super, mrb_symbol(name), FALSE)')
    replace(source, 'method_search_vm(mrb, owner, name)', 'method_search_scoped(mrb, owner, name, TRUE)')
  end

  def self.install_source(original, generated, patch)
    Rake::FileTask.define_task(generated => [original, __FILE__]) do
      source = File.binread(original)
      public_send(patch, source)
      FileUtils.mkdir_p(File.dirname(generated))
      File.binwrite(generated, source)
    end
  end

  def self.install(build)
    output = File.join(build.build_dir, "src")
    overrides = File.join(build.build_dir, "oruby-core")
    inputs = File.join(MRUBY_ROOT, "src")
    { "vm" => :vm, "gc" => :gc, "class" => :const_names, "refinement" => :refinement, "proc" => :proc_identity }.each do |name, patch|
      original = File.join(inputs, "#{name}.c")
      generated = File.join(overrides, "#{name}.c")
      install_source(original, generated, patch)
    end
    build.cc.include_paths << inputs
    build.cc.include_paths << File.expand_path("..", __dir__)
    build.cc.include_paths << File.join(MRUBY_ROOT, "mrbgems/mruby-compiler/lib/prism/include")
    build.cc.define_rules(output, overrides, build.exts.object)
    build.cc.define_rules(output, overrides, build.exts.presym_preprocessed)

    {
      "mruby-objectspace-weakmap" => ["weakmap.c", :weakmap],
      "mruby-method" => ["method.c", :method_lookup]
    }.each do |name, (file, patch)|
      relative = File.join("mrbgems", name, "src")
      gem_output = File.join(build.build_dir, relative)
      gem_overrides = File.join(overrides, relative)
      install_source(File.join(MRUBY_ROOT, relative, file), File.join(gem_overrides, file), patch)
      build.cc.define_rules(gem_output, gem_overrides, build.exts.object)
      build.cc.define_rules(gem_output, gem_overrides, build.exts.presym_preprocessed)
    end
  end
end
