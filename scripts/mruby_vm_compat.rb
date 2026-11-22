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

  def self.missing_dispatch(source)
    replace(source, "mrb_bool super)\n{\n  mrb_sym missing = MRB_SYM(method_missing);",
      "mrb_bool super, uint8_t visibility)\n{\n  mrb_sym missing = MRB_SYM(method_missing);")
    replace(source, '    if (super) mrb_no_method_error_recv', <<~'C'.chomp.gsub(/^/, "    "))
      if (visibility) {
        mrb_no_method_error_recv(mrb, mid, recv, args, "%s method '%n' called for %T",
          visibility == ORUBY_MISSING_PRIVATE ? "private" : "protected", mid, recv);
      }
      if (super) mrb_no_method_error_recv
    C
    replace(source, "  ci->mid = missing;\n  return m;", "  ci->mid = missing;\n  ci->vis |= visibility;\n  return m;")
    replace(source, 'prepare_missing(mrb, ci, self, mid, FALSE)', 'prepare_missing(mrb, ci, self, mid, FALSE, 0)')
    replace(source, 'prepare_missing(mrb, ci, recv, mid, (insn == OP_SUPER))',
      'prepare_missing(mrb, ci, recv, mid, (insn == OP_SUPER), 0)')
    replace(source, <<~'C'.chomp.gsub(/^/, "              "), <<~'C'.chomp.gsub(/^/, "              "))
      mrb_value args = (ci->n == 15) ? regs[1] : mrb_ary_new_from_values(mrb, ci->n, regs+1);
      vis_error(mrb, mid, args, recv, priv);
    C
      m = prepare_missing(mrb, ci, recv, mid, FALSE, priv ? ORUBY_MISSING_PRIVATE : ORUBY_MISSING_PROTECTED);
    C
    replace(source, '      ci->u.target_class = (insn == OP_SUPER) ? CI_TARGET_CLASS(ci - 1)->super : mrb_class(mrb, recv);',
      <<~'C'.chomp.gsub(/^/, "      "))
      if (insn == OP_SUPER) {
        ci->u.target_class = CI_TARGET_CLASS(ci - 1)->super;
        if (mid == MRB_SYM(method_missing)) ci->vis |= ci[-1].vis & ORUBY_MISSING_VISIBILITY;
      }
      else {
        ci->u.target_class = mrb_class(mrb, recv);
      }
    C

    replace(source, <<~'C'.chomp, '')
      static mrb_noreturn void
      vis_error(mrb_state *mrb, mrb_sym mid, mrb_value args, mrb_value recv, mrb_bool priv)
      {
        mrb_no_method_error_recv(mrb, mid, recv, args, "%s method '%n' called for %T", (priv ? "private" : "protected"), mid, recv);
      }
    C
    replace(source, "  if (MRB_METHOD_UNDEF_P(m)) {\n    /* call `method_missing` in this frame:", <<~'C'.chomp.gsub(/^/, "  "))
      uint8_t visibility = 0;
      if (pub && !MRB_METHOD_UNDEF_P(m)) {
        if (m.flags & MRB_METHOD_PRIVATE_FL) visibility = ORUBY_MISSING_PRIVATE;
        else if (m.flags & MRB_METHOD_PROTECTED_FL) visibility = ORUBY_MISSING_PROTECTED;
      }
      if (MRB_METHOD_UNDEF_P(m) || visibility) {
        /* call `method_missing` in this frame:
    C
    replace(source, "    ci->mid = MRB_SYM(method_missing);\n    ci->u.target_class = c;\n    goto call;",
      "    ci->mid = MRB_SYM(method_missing);\n    ci->vis |= visibility;\n    ci->u.target_class = c;\n    goto call;")
    replace(source, <<~'C'.chomp.gsub(/^(?=.)/, "  "), '')
      if (pub) {
        mrb_bool priv = TRUE;
        if (m.flags & MRB_METHOD_PRIVATE_FL) {
        vis_err:;
          if (n == 15) {
            n = (int)(RARRAY_LEN(regs[0]) - 1);
            regs = RARRAY_PTR(regs[0]);
          }
          vis_error(mrb, name, mrb_ary_new_from_values(mrb, n, regs+1), self, priv);
        }
        else if (m.flags & MRB_METHOD_PROTECTED_FL) {
          /* `public_send` rejects protected methods unconditionally */
          priv = FALSE;
          goto vis_err;
        }
      }
    C
  end

  def self.vm(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include \"oruby-vm-compat.h\"")
    replace(source, '    cipush(mrb, 0, CINFO_SKIP, mrb->object_class, NULL, NULL, 0, 0);', <<~'C'.chomp.gsub(/^/, "    "))
      /* A loaded file starts its own visibility scope, like the main file. */
      mrb_bool file_scope = !MRB_PROC_CFUNC_P(proc) && MRB_PROC_CREF_P(proc);
      struct RClass *target = file_scope ? MRB_PROC_TARGET_CLASS(proc) : mrb->object_class;
      mrb_callinfo *ci = cipush(mrb, 0, CINFO_SKIP, target, NULL, NULL, 0, 0);
      if (file_scope) MRB_CI_SET_VISIBILITY(ci, MRB_METHOD_PRIVATE_FL >> 25);
    C
    missing_dispatch(source)
    replace(source, 'argc < 15 && MRB_PROC_STRICT_P(ci->proc)) {',
      'argc < 15 && !ci->kw && MRB_PROC_STRICT_P(ci->proc)) {')
    replace(source, 'argc+ci->kw != m1)) { /* count kdict too */', 'argc != m1)) {')
    replace(source, <<~'C'.chomp.gsub(/^(?=.)/, "    "), <<~'C'.chomp.gsub(/^(?=.)/, "    "))
      if (mrb_array_p(stack[m1])) {
        struct RArray *ary = mrb_ary_ptr(stack[m1]);

        pp = ARY_PTR(ary);
        len = ARY_LEN(ary);
      }
    C
      mrb_value rest_value = stack[m1];
      if (!mrb_array_p(rest_value) && !mrb_nil_p(rest_value)) {
        rest_value = mrb_ary_splat(mrb, rest_value);
        ci = mrb->c->ci;
        stack = lv == 0 ? regs + 1 : uvenv(mrb, lv-1)->stack + 1;
      }
      if (mrb_array_p(rest_value)) {
        pp = RARRAY_PTR(rest_value);
        len = RARRAY_LEN(rest_value);
      }
    C
    replace(source, '      if (vm_op_argary(mrb, a, b) == VM_RAISE) goto L_RAISE;',
      "      if (vm_op_argary(mrb, a, b) == VM_RAISE) goto L_RAISE;\n      ci = mrb->c->ci;")
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
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include \"oruby-vm-compat.h\"")
    class_method_definitions(source)
    replace(source, "static mrb_value\ntop_public(mrb_state *mrb, mrb_value self)", <<~'C'.chomp)
      static struct RClass *
      top_visibility_target(mrb_state *mrb, mrb_value self)
      {
        /* Only the copy of main supplied by a wrapped load carries a
           module. An ordinary clone still addresses Object. */
        if (!mrb_obj_eq(mrb, self, mrb_top_self(mrb))) {
          mrb_value target = mrb_iv_get(mrb, self, MRB_SYM(__oruby_load_wrapper));
          if (mrb_module_p(target)) return mrb_class_ptr(target);
        }
        return mrb->object_class;
      }

      static mrb_value
      top_public(mrb_state *mrb, mrb_value self)
    C
    %w[MRB_METHOD_PUBLIC_FL MRB_METHOD_PRIVATE_FL MT_PROTECTED].each do |visibility|
      replace(source, "  return mrb_mod_visibility(mrb, mrb_obj_value(mrb->object_class), #{visibility});",
        "  return mrb_mod_visibility(mrb, mrb_obj_value(top_visibility_target(mrb, self)), #{visibility});")
    end
    replace(source, '  mrb_method_missing(mrb, name, mod, mrb_ary_new_from_values(mrb, alen, a));', <<~'C'.chomp.gsub(/^/, "  "))
      mrb_value args = mrb_ary_new_from_values(mrb, alen, a);
      uint8_t visibility = mrb->c->ci->vis & ORUBY_MISSING_VISIBILITY;
      if (visibility) {
        mrb_no_method_error_recv(mrb, name, mod, args, "%s method '%n' called for %T",
          visibility == ORUBY_MISSING_PRIVATE ? "private" : "protected", name, mod);
      }
      mrb_method_missing(mrb, name, mod, args);
    C
    replace(source, '    mrb_check_frozen_value(mrb, v);', <<~'C'.chomp.gsub(/^/, "    "))
      mrb_check_frozen_value(mrb, v);
      mrb_raisef(mrb, E_FROZEN_ERROR, "can't modify frozen %T: %v", v, v);
    C
    inherited_visibility(source)
    replace(source, '  return len > 0 && ISUPPER(name[0]) && mrb_ident_p(name+1, len-1);', <<~'C'.chomp.gsub(/^/, "  "))
      if (len <= 0) return FALSE;
      if (ISASCII(name[0])) return ISUPPER(name[0]) && mrb_ident_p(name+1, len-1);
      return pm_encoding_utf_8_isupper_char((const uint8_t*)name, len) &&
             mrb_ident_p(name+1, len-1);
    C
  end

  def self.class_method_definitions(source)
    # Resolve module_function scope before enforcing private instance methods.
    # The singleton copy keeps its own public visibility.
    replace(source, <<~'C'.gsub(/^/, "  "), "  if ((flags & MT_VMASK) == MT_VDEFAULT) {\n")
      if (mid == MRB_SYM(initialize) ||
          mid == MRB_SYM(initialize_copy) ||
          mid == MRB_SYM_Q(respond_to_missing)) {
        MRB_SET_VISIBILITY_FLAGS(flags, MRB_METHOD_PRIVATE_FL);
      }
      else if ((flags & MT_VMASK) == MT_VDEFAULT) {
    C
    replace(source, "  mt_put(mrb, h, mid, flags, ptr);\n  if (!mrb->bootstrapping) {", <<~'C'.chomp.gsub(/^/, "  "))
      if (named->tt != MRB_TT_SCLASS &&
          (mid == MRB_SYM(initialize) || mid == MRB_SYM(initialize_copy) ||
           mid == MRB_SYM_Q(respond_to_missing))) {
        MRB_SET_VISIBILITY_FLAGS(flags, MRB_METHOD_PRIVATE_FL);
      }
      mt_put(mrb, h, mid, flags, ptr);
      if (!mrb->bootstrapping) {
    C
    # Keep initialize and its block in the VM. Only the allocator changes.
    replace(source, "OP_SSEND     R4  :allocate  n=0", "OP_SSEND     R4  :__oruby_allocate_instance  n=0")
    replace(source, "MRB_PRESYM_DEFINE_VAR_AND_INITER(new_syms, 2, MRB_SYM(allocate), MRB_SYM(initialize))",
      "MRB_PRESYM_DEFINE_VAR_AND_INITER(new_syms, 2, MRB_SYM(__oruby_allocate_instance), MRB_SYM(initialize))")
    replace(source, "  MRB_PRESYM_INIT_SYMBOLS(mrb, new_syms);", <<~'C'.chomp.gsub(/^/, "  "))
      MRB_PRESYM_INIT_SYMBOLS(mrb, new_syms);
      mrb_define_private_method_id(mrb, cls, MRB_SYM(__oruby_allocate_instance), mrb_instance_alloc, MRB_ARGS_NONE());
    C
  end

  def self.inherited_visibility(source)
    replace(source, "mrb_method_t\nmrb_vm_find_method(mrb_state *mrb,", <<~'C'.chomp)
      static mrb_value
      oruby_visibility_entry(mrb_state *mrb, mrb_value self)
      {
        mrb_raise(mrb, E_RUNTIME_ERROR, "unresolved inherited method");
      }

      void
      oruby_set_inherited_visibility(mrb_state *mrb, struct RClass *c, mrb_sym mid, uint32_t visibility)
      {
        mrb_method_t m;
        MRB_METHOD_FROM_FUNC(m, oruby_visibility_entry);
        m.flags |= ORUBY_METHOD_VISIBILITY_ONLY;
        MRB_METHOD_SET_VISIBILITY(m, visibility);
        mrb_define_method_raw(mrb, c, mid, m);
      }

      /* Reflection reports the class that declared visibility. The VM keeps
         the implementation owner so a super in the body starts there. */
      struct RClass *
      oruby_visibility_owner(mrb_state *mrb, struct RClass *c, struct RClass *implementation, mrb_sym mid)
      {
      #ifdef MRB_USE_REFINEMENTS
        if (MRB_CLASS_REFINEMENT_P(implementation)) return implementation;
      #endif
        for (; c && c != implementation; c = c->super) {
          union mrb_mt_ptr ptr;
          uint32_t flags;
          if (!c->mt || !mt_get(mrb, c->mt, mid, &ptr, &flags)) continue;
          if (flags & ORUBY_METHOD_VISIBILITY_ONLY) return c;
          break;
        }
        return implementation;
      }

      mrb_method_t
      mrb_vm_find_method(mrb_state *mrb,
    C
    replace(source, <<~'C'.chomp.gsub(/^(?=.)/, "  "), <<~'C'.chomp.gsub(/^(?=.)/, "  "))
      while (c) {
        mrb_mt_tbl *h = c->mt;

        if (h) {
          union mrb_mt_ptr ptr;
          uint32_t flags;
          if (mt_get(mrb, h, mid, &ptr, &flags)) {
            if (ptr.proc == 0) break;
            *cp = c;
            m = create_method_value(mrb, flags, ptr);
    C
      uint32_t visibility = MT_VDEFAULT;
      while (c) {
        mrb_mt_tbl *h = c->mt;

        if (h) {
          union mrb_mt_ptr ptr;
          uint32_t flags;
          if (mt_get(mrb, h, mid, &ptr, &flags)) {
            if (ptr.proc == 0) break;
            if (flags & ORUBY_METHOD_VISIBILITY_ONLY) {
              if (visibility == MT_VDEFAULT) visibility = flags & MT_VMASK;
              c = c->super;
              continue;
            }
            if (visibility != MT_VDEFAULT) MRB_SET_VISIBILITY_FLAGS(flags, visibility);
            *cp = c;
            m = create_method_value(mrb, flags, ptr);
    C
    replace(source, "  uint32_t flags;\n\n  while (c) {\n    struct RClass *key = c;",
      "  uint32_t flags;\n  uint32_t visibility = MT_VDEFAULT;\n\n  while (c) {\n    struct RClass *key = c;")
    replace(source, '        return create_method_value(mrb, flags, ptr);',
      "        if (visibility != MT_VDEFAULT) MRB_SET_VISIBILITY_FLAGS(flags, visibility);\n" \
      "        return create_method_value(mrb, flags, ptr);")
    replace(source, <<~'C'.chomp.gsub(/^/, "    "), <<~'C'.chomp.gsub(/^/, "    "))
      if (h && mt_get(mrb, h, mid, &ptr, &flags)) {
        if (ptr.proc == 0) break;
        *cp = c;
        return create_method_value(mrb, flags, ptr);
      }
    C
      if (h && mt_get(mrb, h, mid, &ptr, &flags)) {
        if (ptr.proc == 0) break;
        if (flags & ORUBY_METHOD_VISIBILITY_ONLY) {
          if (visibility == MT_VDEFAULT) visibility = flags & MT_VMASK;
          c = c->super;
          continue;
        }
        if (visibility != MT_VDEFAULT) MRB_SET_VISIBILITY_FLAGS(flags, visibility);
        *cp = c;
        return create_method_value(mrb, flags, ptr);
      }
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
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include \"oruby-vm-compat.h\"")
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
    replace(source, '  while ((owner)->tt == MRB_TT_ICLASS)', <<~'C'.chomp.gsub(/^/, "  "))
      struct RClass *visibility_owner = oruby_visibility_owner(mrb, c, owner, name);
      if (visibility_owner != owner && proc && !MRB_PROC_CFUNC_P(proc) && !MRB_PROC_ALIAS_P(proc)) {
        /* Capture the body for this Method, including the owner used by super. */
        struct RProc *alias = MRB_OBJ_ALLOC(mrb, MRB_TT_PROC, mrb->proc_class);
        alias->body.mid = name;
        alias->upper = proc;
        alias->e.target_class = owner;
        alias->flags |= MRB_PROC_ALIAS;
        proc = alias;
      }
      owner = visibility_owner;
      while ((owner)->tt == MRB_TT_ICLASS)
    C
  end

  def self.eval_source(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include \"oruby-vm-compat.h\"")
    replace(source, '  proc = mrb_generate_code(mrb, p);', <<~'C'.chomp.gsub(/^/, "  "))
      proc = mrb_generate_code(mrb, p);
      if (proc) {
        uint8_t flags = ORUBY_IREP_EVAL;
        mrb_value kernel = mrb_obj_value(mrb->kernel_module);
        if (mrb_test(mrb_iv_get(mrb, kernel, MRB_IVSYM(__oruby_keep_script_lines)))) {
          flags |= ORUBY_IREP_RETAINED_SOURCE;
        }
        oruby_eval_irep_flags((mrb_irep*)proc->body.irep, flags);
      }
    C
    replace(source, '/* provided by mruby-binding */', <<~'C'.chomp)
      /* Only freshly compiled IREPs reach this path. Methods and blocks
         compiled by eval keep their origin after its frame has returned. */
      static void
      oruby_eval_irep_flags(mrb_irep *irep, uint8_t flags)
      {
        irep->flags |= flags;
        for (uint16_t i = 0; i < irep->rlen; ++i) {
          oruby_eval_irep_flags((mrb_irep*)irep->reps[i], flags);
        }
      }

      /* provided by mruby-binding */
    C
  end

  def self.array_splat(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include <mruby/variable.h>")
    replace(source, '  if (!mrb_respond_to(mrb, v, MRB_SYM(to_a))) {', <<~'C'.chomp.gsub(/^/, "  "))
      if (mrb_nil_p(v)) return mrb_ary_new(mrb);
      mrb_sym to_a = MRB_SYM(to_a);
      mrb_sym respond_to = MRB_SYM_Q(respond_to);
      struct RClass *owner = mrb_class(mrb, v);
      mrb_method_t method = mrb_method_search_vm(mrb, &owner, respond_to);
      mrb_bool responds;
      if (!MRB_METHOD_UNDEF_P(method)) {
        const mrb_value args[] = {mrb_symbol_value(to_a), mrb_true_value()};
        mrb_int argc = 2;
        if (MRB_METHOD_PROC_P(method) && mrb_proc_arity(MRB_METHOD_PROC(method)) == 1) {
          if (mrb_test(mrb_gv_get(mrb, MRB_GVSYM(VERBOSE)))) {
            mrb_warn(mrb, "%T#respond_to?(:to_a) uses the deprecated method signature, which takes one parameter", v);
          }
          argc = 1;
        }
        responds = mrb_test(mrb_funcall_argv(mrb, v, respond_to, argc, args));
      }
      else {
        responds = mrb_respond_to(mrb, v, to_a);
      }
      if (!responds) {
    C
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
    {
      "vm" => :vm, "gc" => :gc, "class" => :const_names, "refinement" => :refinement,
      "proc" => :proc_identity, "array" => :array_splat
    }.each do |name, patch|
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
      "mruby-method" => ["method.c", :method_lookup],
      "mruby-eval" => ["eval.c", :eval_source]
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
