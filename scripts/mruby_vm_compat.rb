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
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include <mruby/debug.h>\n#include \"oruby-vm-compat.h\"")
    replace(source, '    cipush(mrb, 0, CINFO_SKIP, mrb->object_class, NULL, NULL, 0, 0);', <<~'C'.chomp.gsub(/^/, "    "))
      /* A loaded file starts its own visibility scope, like the main file. */
      mrb_bool file_scope = !MRB_PROC_CFUNC_P(proc) && MRB_PROC_CREF_P(proc);
      struct RClass *target = file_scope ? MRB_PROC_TARGET_CLASS(proc) : mrb->object_class;
      mrb_callinfo *ci = cipush(mrb, 0, CINFO_SKIP, target, NULL, NULL, 0, 0);
      if (file_scope) MRB_CI_SET_VISIBILITY(ci, MRB_METHOD_PRIVATE_FL >> 25);
    C
    missing_dispatch(source)
    replace(source, "static int\nvm_op_enter(", <<~'C'.chomp)
      static mrb_bool
      oruby_irep_uses_block(const mrb_irep *irep)
      {
        const mrb_code *end = irep->iseq + irep->ilen;
        for (const mrb_code *pc = irep->iseq; pc < end;) {
          struct mrb_insn_data insn = mrb_decode_insn(pc);
          if (insn.insn == OP_ENTER && MRB_ASPEC_BLOCK(insn.a)) return TRUE;
          if (insn.insn == OP_SUPER || insn.insn == OP_BLKPUSH || insn.insn == OP_ARGARY) return TRUE;
          if ((insn.insn == OP_BLOCK || insn.insn == OP_LAMBDA) && insn.b < irep->rlen &&
              oruby_irep_uses_block(irep->reps[insn.b])) return TRUE;
          pc = insn.next;
        }
        return FALSE;
      }

      static void
      oruby_warn_unused_block(mrb_state *mrb, mrb_callinfo *ci, const struct RProc *proc)
      {
        mrb_irep *irep = (mrb_irep*)proc->body.irep;
        if (!irep || !MRB_PROC_CREF_P(proc) || ci->mid == MRB_SYM(initialize) ||
            (irep->flags & (ORUBY_IREP_BLOCK_WARNED | MRB_IREP_NO_FREE))) return;
        const mrb_callinfo *caller = ci - 1;
        if (!caller->proc || MRB_PROC_CFUNC_P(caller->proc) || !caller->pc) return;
        const mrb_irep *from = caller->proc->body.irep;
        int32_t line, defined_line;
        const char *file, *defined_file;
        if (!mrb_debug_get_position(mrb, from, (uint32_t)(caller->pc - from->iseq - 1), &line, &file) || !file ||
            !mrb_debug_get_position(mrb, irep, 0, &defined_line, &defined_file) || !defined_file ||
            strncmp(file, "<internal:", 10) == 0 || strncmp(defined_file, "<internal:", 10) == 0 ||
            oruby_irep_uses_block(irep)) return;

        mrb_value kernel = mrb_obj_value(mrb->kernel_module);
        if (!mrb_respond_to(mrb, kernel, MRB_SYM(warn))) return;
        irep->flags |= ORUBY_IREP_BLOCK_WARNED;
        int arena = mrb_gc_arena_save(mrb);
        mrb_value message = mrb_format(mrb, "%s:%d: warning: the block passed to '%n' defined at %s:%d may be ignored",
                                       file, (int)line, ci->mid, defined_file, (int)defined_line);
        mrb_funcall_argv1(mrb, kernel, MRB_SYM(warn), message);
        mrb_gc_arena_restore(mrb, arena);
      }

      static int
      vm_op_enter(
    C
    replace(source, "          /* setup environment for calling method */\n          irep = p->body.irep;\n          stack_extend(mrb, (irep->nregs < 4) ? 4 : irep->nregs);\n          ci->pc = irep->iseq;", <<~'C'.chomp.gsub(/^/, "          "))
      /* setup environment for calling method */
      irep = p->body.irep;
      stack_extend(mrb, (irep->nregs < 4) ? 4 : irep->nregs);
      ci->pc = irep->iseq;
      if (mrb_unlikely(ci->blk && insn != OP_SUPER && mrb->kernel_module &&
                       (mrb->kernel_module->flags & ORUBY_KERNEL_UNUSED_BLOCK_WARNING))) {
        oruby_warn_unused_block(mrb, ci, p);
        ci = mrb->c->ci;
      }
    C
    replace(source, "    {\n      mrb_method_t m;\n\n      CHECK_VM_INTERRUPT(mrb);", <<~'C'.chomp.gsub(/^/, "    "))
      {
        mrb_method_t m;
        mrb_bool case_equal = mid == MRB_SYM(__oruby_case_equal);
        if (case_equal) mid = MRB_OPSYM(eqq);

        CHECK_VM_INTERRUPT(mrb);
    C
    replace(source, '        if (mrb_unlikely(m.flags & (MRB_METHOD_PRIVATE_FL | MRB_METHOD_PROTECTED_FL))) {',
      '        if (mrb_unlikely(m.flags & (MRB_METHOD_PRIVATE_FL | MRB_METHOD_PROTECTED_FL)) && !case_equal) {')
    replace(source, '    if (mrb_unlikely(argc < m1 + m2 || (r == 0 && argc > len))) {', <<~'C'.chomp.gsub(/^/, "    "))
      mrb_bool implicit = r && irep->lv && m1 + o < irep->nlocals - 1 &&
                         irep->lv[m1 + o] == MRB_SYM(__oruby_implicit_rest);
      if (mrb_unlikely(argc < m1 + m2 || ((!r || implicit) && argc > len - implicit))) {
    C
    replace(source, "    CASE(OP_DEBUG, BBB) {", <<~'C'.chomp.gsub(/^/, "    "))
      CASE(OP_DEBUG, BBB) {
        if (b == ORUBY_CHILLED_DEBUG_TAG && c <= 1 && mrb_string_p(regs[a])) {
          if (c && irep->debug_info) {
            const char *file;
            int32_t line;
            if (mrb_debug_get_position(mrb, irep, (uint32_t)(ci->pc - irep->iseq - 1), &line, &file) && file) {
              int arena = mrb_gc_arena_save(mrb);
              mrb_value origin = mrb_format(mrb, "%s:%d", file, (int)line);
              mrb_iv_set(mrb, regs[a], MRB_SYM(__oruby_frozen_origin), origin);
              mrb_gc_arena_restore(mrb, arena);
            }
          }
          mrb_basic_ptr(regs[a])->flags |= ORUBY_STRING_CHILLED;
          NEXT;
        }
    C
    replace(source, '      regs[b] = mrb_bool_value(!mrb_break_p(exc) && mrb_obj_is_kind_of(mrb, exc, ec));', <<~'C'.chomp.gsub(/^/, "      "))
      mrb_bool matches = FALSE;
      if (!mrb_break_p(exc)) {
        if (mrb_func_basic_p(mrb, e, MRB_OPSYM(eqq), mrb_mod_eqq)) {
          matches = mrb_obj_is_kind_of(mrb, exc, ec);
        }
        else {
          matches = mrb_test(mrb_funcall_id(mrb, e, MRB_OPSYM(eqq), 1, exc));
          ci = mrb->c->ci;
          regs = ci->stack;
        }
      }
      regs[b] = mrb_bool_value(matches);
    C
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
          mrb_raisef(mrb, E_TYPE_ERROR, "superclass must be a Class (%T given)", super);
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
    svar_scope_frame(source)
  end

  # $~ and $_ after a bare private/public/protected/module_function: that
  # gives the frame's scope proc the env of its own frame (class.c's
  # vis_scope_persist), so svar_own_scope_p() no longer holds for it and the
  # walk looked for the env's frame below the frame itself: a read answered
  # nil and a write was dropped, while its blocks resolve to the frame.
  def self.svar_scope_frame(source)
    replace(source, "      if (svar_own_scope_p(p)) {\n",
      "      if (svar_own_scope_p(p) || (MRB_PROC_SCOPE_P(p) && MRB_PROC_ENV(p) == CI_ENV(ci))) {\n")
    strcat_private_to_s(source)
    class_block_return(source)
    gvar_uninitialized_warning(source)
    # a module method bound to an unrelated receiver runs under an ICLASS whose
    # superclass is the receiver's class (mruby-method's mcall): self is a kind
    # of that class
    replace(source, "      if (!mrb_obj_is_kind_of(mrb, recv, target_class)) {\n      super_typeerror:",
      "      if (!mrb_obj_is_kind_of(mrb, recv, target_class) &&\n" \
      "          !(target_class->tt == MRB_TT_ICLASS && target_class->super && mrb_obj_is_kind_of(mrb, recv, target_class->super))) {\n" \
      "      super_typeerror:")
  end

  # reading a global variable never assigned warns in verbose mode, as CRuby;
  # the test runs only for a nil value
  def self.gvar_uninitialized_warning(source)
    replace(source, "      mrb_value val = mrb_gv_get(mrb, irep->syms[b]);\n      ci = mrb->c->ci;\n", <<~'C'.gsub(/^/, "      "))
      mrb_value val = mrb_gv_get(mrb, irep->syms[b]);
      /* not for $g ||= v, whose read is followed by its test (CRuby checks defined?) */
      if (mrb_unlikely(mrb_nil_p(val)) && ci->pc[0] != OP_JMPIF && mrb_true_p(mrb_gv_get(mrb, MRB_GVSYM(VERBOSE))) &&
          !mrb_gv_defined(mrb, irep->syms[b]) &&
          mrb_respond_to(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__oruby_warn_here))) {
        mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__oruby_warn_here), 1,
                       mrb_format(mrb, "global variable '%n' not initialized", irep->syms[b]));
      }
      ci = mrb->c->ci;
    C
  end

  # return in a block whose home is a class body (an OP_EXEC frame: no method
  # name, a scope proc with an upper one, unlike the top level) is a
  # LocalJumpError in CRuby; mruby returned from the class body
  def self.class_block_return(source)
    replace(source, "          if (ci->u.env == env) {\n            goto L_UNWINDING;\n", <<~'C'.gsub(/^/, "          "))
                if (ci->u.env == env) {
                  if (ci->mid == 0 && ci->proc && !MRB_PROC_CFUNC_P(ci->proc) &&
                      MRB_PROC_SCOPE_P(ci->proc) && ci->proc->upper) break;
                  goto L_UNWINDING;
    C
  end

  # "#{obj}" calls obj's #to_s even when it is private, as CRuby's
  # rb_obj_as_string does: the send in OP_STRCAT's slow path (the static
  # prepare_exec_strcat_irep) is exempt from the visibility check.
  def self.strcat_private_to_s(source)
    replace(source, "          if (insn == OP_SEND || insn == OP_SEND0 || insn == OP_SENDB) {\n            exempt = FALSE;",
      "          if (insn == OP_SEND || insn == OP_SEND0 || insn == OP_SENDB) {\n            exempt = (irep == &prepare_exec_strcat_irep);")
  end

  def self.const_names(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include <prism/internal/encoding.h>")
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include \"oruby-vm-compat.h\"")
    class_method_definitions(source)
    replace(source, "static mrb_value\nmrb_mod_eqq(mrb_state *mrb,", "mrb_value\nmrb_mod_eqq(mrb_state *mrb,")
    replace(source, '  if (o->c->tt == MRB_TT_SCLASS) return;', <<~'C'.chomp.gsub(/^/, "  "))
      if (o->c->tt == MRB_TT_SCLASS) return;
      if (o->tt == MRB_TT_STRING && (o->flags & ORUBY_STRING_CHILLED) && !mrb_frozen_p(o)) {
        oruby_chilled_mutation(mrb, o);
      }
    C
    replace(source, '  p->frozen = mrb_obj_ptr(self)->frozen;', <<~'C'.chomp.gsub(/^/, "  "))
      p->frozen = mrb_obj_ptr(self)->frozen;
      if (mrb_string_p(self)) p->flags |= mrb_basic_ptr(self)->flags & ORUBY_STRING_CHILLED;
    C
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
    metaclass_class(source)
    bop_redefined_warning(source)
    prepend_fixes(source)
    # the message names the class: a BasicObject superclass has no #inspect
    # (go-mrb.c's classdef_fix gives MRI's words)
    replace(source, '      mrb_raisef(mrb, E_TYPE_ERROR, "superclass must be a Class (%!v given)", super);',
      '      mrb_raisef(mrb, E_TYPE_ERROR, "superclass must be a Class (%T given)", super);')
  end

  # The class of a metaclass, as MRI's meta^n chain: mruby gives every new
  # singleton class Class as its class, so a metaclass without a singleton of
  # its own missed the methods of #<Class:Class> (and of a superclass's
  # metaclass of a metaclass), and #<Class:A>.is_a?(#<Class:Class>) was false.
  # Such a metaclass now has as its class the singleton of the nearest
  # metaclass up its superclass chain that has one (#<Class:Class> at the top);
  # one gained later refreshes those below it. "Has a singleton of its own" is
  # therefore o->c's __attached__ being o, not o->c being a singleton class.
  def self.metaclass_class(source)
    replace(source, "/* Prepares and creates a singleton class for an object */\nstatic void\nprepare_singleton_class(", <<~'C'.chomp)
      mrb_bool
      oruby_own_sclass_p(mrb_state *mrb, const struct RBasic *o)
      {
        struct RClass *sc = o->c;

        if (!sc || sc->tt != MRB_TT_SCLASS) return FALSE;
        if (o->tt != MRB_TT_CLASS && o->tt != MRB_TT_SCLASS) return TRUE;
        mrb_value a = mrb_obj_iv_get(mrb, (struct RObject*)sc, MRB_SYM(__attached__));
        return !mrb_immediate_p(a) && mrb_basic_ptr(a) == o;
      }

      /* the singleton class of a class, or of such a singleton class */
      static mrb_bool
      oruby_metaclass_p(mrb_state *mrb, struct RClass *s)
      {
        if (s->tt != MRB_TT_SCLASS) return FALSE;
        mrb_value a = mrb_obj_iv_get(mrb, (struct RObject*)s, MRB_SYM(__attached__));
        return mrb_type(a) == MRB_TT_CLASS || mrb_type(a) == MRB_TT_SCLASS;
      }

      /* The class of metaclass s that has no singleton class of its own: the
         singleton of the nearest metaclass up s's superclass chain that has one,
         or, above the metaclasses, the class of Class (#<Class:Class>). */
      static struct RClass*
      oruby_metaclass_class(mrb_state *mrb, struct RClass *s)
      {
        for (struct RClass *x = s->super; x; x = x->super) {
          if (x->tt == MRB_TT_ICLASS) continue;
          if (x->tt != MRB_TT_SCLASS) return x->c ? x->c : mrb->class_class;
          if (oruby_own_sclass_p(mrb, (struct RBasic*)x)) return x->c;
        }
        return mrb->class_class;
      }

      static int
      oruby_metaclass_refresh_i(mrb_state *mrb, struct RBasic *obj, void *ud)
      {
        (void)ud;
        if (obj->tt != MRB_TT_SCLASS) return MRB_EACH_OBJ_OK;
        struct RClass *s = (struct RClass*)obj;
        if (!s->c || oruby_own_sclass_p(mrb, obj) || !oruby_metaclass_p(mrb, s)) return MRB_EACH_OBJ_OK;
        struct RClass *k = oruby_metaclass_class(mrb, s);
        if (k != s->c) {
          s->c = k;
          mrb_field_write_barrier(mrb, obj, (struct RBasic*)k);
        }
        return MRB_EACH_OBJ_OK;
      }

      /* Prepares and creates a singleton class for an object */
      static void
      prepare_singleton_class(
    C
    replace(source, '  if (o->c->tt == MRB_TT_SCLASS) return;', '  if (oruby_own_sclass_p(mrb, o)) return;')
    replace(source, "  sc->frozen = o->frozen;\n}", <<~'C'.chomp)
        sc->frozen = o->frozen;
        if (o->tt == MRB_TT_CLASS || oruby_metaclass_p(mrb, (struct RClass*)o)) {
          sc->c = oruby_metaclass_class(mrb, sc);
          mrb_field_write_barrier(mrb, (struct RBasic*)sc, (struct RBasic*)sc->c);
          /* the metaclasses below a new meta^n class (and all of them, once
             Class has its own) take it as their class; it is rare, so a walk
             over the heap does */
          if (o->tt == MRB_TT_SCLASS || o == (struct RBasic*)mrb->class_class) {
            mrb_gc_each_live_object(mrb, oruby_metaclass_refresh_i, NULL);
          }
        }
      }
    C
    # A metaclass's class is now #<Class:Class>, where mruby defines Class.new
    replace(source, "mrb_class_new_class(mrb_state *mrb, mrb_value cv)\n{\n  mrb_value super, blk;\n", <<~'C')
      mrb_class_new_class(mrb_state *mrb, mrb_value cv)
      {
        mrb_value super, blk;
        if (mrb_sclass_p(cv)) mrb_raise(mrb, E_TYPE_ERROR, "can't create instance of singleton class");
    C
    replace(source, "  struct RClass *klass = mrb_basic_ptr(obj)->c;\n\n  if (klass->tt != MRB_TT_SCLASS)\n    return klass;",
      "  struct RClass *klass = mrb_basic_ptr(obj)->c;\n\n  if (!oruby_own_sclass_p(mrb, mrb_basic_ptr(obj)))\n    return klass;")
    replace(source, "    clone->super = klass->super;\n", <<~'C'.gsub(/^/, "    "))
      clone->super = klass->super;
      if (mrb_type(obj) == MRB_TT_CLASS || mrb_type(obj) == MRB_TT_SCLASS) {
        clone->c = oruby_metaclass_class(mrb, clone);
      }
    C
    # A large Integer has no singleton class, as in MRI
    replace(source, "  case MRB_TT_INTEGER:\n#ifndef MRB_NO_FLOAT\n  case MRB_TT_FLOAT:\n#endif\n    return NULL;",
      "  case MRB_TT_INTEGER:\n#ifdef MRB_USE_BIGINT\n  case MRB_TT_BIGINT:\n#endif\n#ifndef MRB_NO_FLOAT\n  case MRB_TT_FLOAT:\n#endif\n    return NULL;")
  end

  # kind_of?: an include class shares its module's method table, which is
  # what the test compares; tables of two classes without methods are both
  # NULL, so a metaclass (whose chain now runs through the method-less
  # #<Class:Object>) was "a kind of" every method-less singleton class
  def self.kind_of_mt(source)
    replace(source, '    if (cl == c || cl->mt == c->mt)',
      '    if (cl == c || (cl->tt == MRB_TT_ICLASS && cl->c == c) || (cl->mt && cl->mt == c->mt))')
    # a conversion method's wrong result (&obj's #to_proc, ...): CRuby's message
    replace(source, '    mrb_raisef(mrb, E_TYPE_ERROR, "%v cannot be converted to %s by #%n", val, tname, method);',
      '    mrb_raisef(mrb, E_TYPE_ERROR, "can\'t convert %T into %s (%T#%n gives %T)", val, tname, val, method, v);')
  end

  # Module#dup of a prepended module: the copy keeps the prepended chain it
  # was given, whose origin stands for the copy (the last line put the
  # original's chain back). prepend: a module already further up (included
  # with another module's prepend) is prepended again, as CRuby checks only
  # the class's own prepended part, up to its origin.
  def self.prepend_fixes(source)
    replace(source, "    c1->super = mrb_class_ptr(mrb_obj_dup(mrb, mrb_obj_value(c0)));\n    c1->super->flags |= MRB_FL_CLASS_IS_ORIGIN;\n  }",
      "    c1->super = mrb_class_ptr(mrb_obj_dup(mrb, mrb_obj_value(c0)));\n    c1->super->flags |= MRB_FL_CLASS_IS_ORIGIN;\n" \
      "    c1->super->c = dc;\n    mrb_field_write_barrier(mrb, (struct RBasic*)c1->super, (struct RBasic*)dc);\n  }")
    replace(source, "  dc->super = sc->super;\n  dc->flags = sc->flags;\n  dc->frozen = 0;\n}",
      "  if (!(sc->flags & MRB_FL_CLASS_IS_PREPENDED)) dc->super = sc->super;\n  dc->flags = sc->flags;\n  dc->frozen = 0;\n}")
    replace(source, "  void *klass_mt = find_origin(c)->mt;\n",
      "  void *klass_mt = find_origin(c)->mt;\n  mrb_bool prepending = !search_super && ins_pos == c;\n")
    replace(source, "    while (p) {\n      if (c == p) original_seen = TRUE;\n",
      "    while (p) {\n      if (c == p) original_seen = TRUE;\n      if (prepending && (p->flags & MRB_FL_CLASS_IS_ORIGIN)) break;\n")
  end

  # CRuby's performance warning when an optimised operator is redefined;
  # Kernel#warn drops it unless Warning[:performance] is on
  def self.bop_redefined_warning(source)
    replace(source, "  else {\n    mrb->bop_redefined |= 1u << slot;\n  }\n}", <<~'C'.chomp)
      else {
        mrb_bool was = (mrb->bop_redefined & (1u << slot)) != 0;
        mrb->bop_redefined |= 1u << slot;
        mrb_value kernel = mrb_obj_value(mrb->kernel_module);
        if (!was && c && mrb_respond_to(mrb, kernel, MRB_SYM(__oruby_warn_here))) {
          mrb_value msg = mrb_format(mrb, "Redefining '%C#%n' disables interpreter and JIT optimizations",
                                     bop_class(mrb, slot), bop_mid(slot));
          mrb_funcall_id(mrb, kernel, MRB_SYM(__oruby_warn_here), 2, msg, mrb_symbol_value(MRB_SYM(performance)));
        }
      }
    }
    C
  end

  # Object#freeze: a metaclass's inherited class is not its own to freeze
  def self.metaclass_freeze(source)
    replace(source, '      if (b->c->tt == MRB_TT_SCLASS) b->c->frozen = 1;', '      if (oruby_own_sclass_p(mrb, b)) b->c->frozen = 1;')
    block_given_define_method(source)
  end

  # block_given? in a define_method body: the block installed as a method is
  # marked a scope but keeps the env it was written in, and the block given
  # to that method's call is not the one asked about. As in CRuby, the
  # answer is the enclosing scope's, so the walk goes on through that env.
  def self.block_given_define_method(source)
    replace(source, '    if (MRB_PROC_SCOPE_P(p)) break;', '    if (MRB_PROC_SCOPE_P(p) && !MRB_PROC_ENV_P(p)) break;')
  end

  # singleton_methods: an inherited metaclass class holds none of the object's
  def self.metaclass_singleton_methods(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include \"oruby-vm-compat.h\"")
    replace(source, "  if (klass && (klass->tt == MRB_TT_SCLASS)) {",
      "  if (klass && klass->tt == MRB_TT_SCLASS && oruby_own_sclass_p(mrb, mrb_basic_ptr(obj))) {")
    # the hook stays private, as class.c defines it and CRuby has it
    replace(source, "MRB_SYM(method_removed), MRB_ARGS_REQ(1)),", "MRB_SYM(method_removed), MRB_ARGS_REQ(1) | MRB_MT_PRIVATE),")
  end

  def self.defined_queries(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include <mruby/debug.h>\n#include \"oruby-vm-compat.h\"")
    replace(source, '  if (mid != 0 && tc != NULL && tc->super != NULL) {', <<~'C'.chomp.gsub(/^/, "  "))
      /* A block in define_method closes over the installed method frame.
         Its own callinfo can still carry the class from the original block. */
      const struct RProc *p = ci->proc;
      while (p && !MRB_PROC_CFUNC_P(p) && !MRB_PROC_SCOPE_P(p) && p->upper) {
        const struct RProc *upper = p->upper;
        struct REnv *env = MRB_PROC_ENV(p);
        if (MRB_PROC_SCOPE_P(upper) && MRB_PROC_ENV_P(upper)) {
          if (env && env->mid) {
            mid = env->mid;
            tc = env->c;
          }
          for (mrb_callinfo *frame = ci - 1; frame >= mrb->c->cibase; frame--) {
            if (frame->proc == upper) {
              mid = frame->mid;
              tc = mrb_vm_ci_target_class(frame);
              break;
            }
          }
          break;
        }
        p = upper;
      }
      if (mid != 0 && tc != NULL && tc->super != NULL) {
    C
    replace(source, '    mrb_sym rtm_id = MRB_SYM_Q(respond_to_missing);', <<~'C'.chomp.gsub(/^/, "    "))
      mrb_sym rtm_id = MRB_SYM_Q(respond_to_missing);
      mrb_sym rt_id = MRB_SYM_Q(respond_to);
      struct RClass *owner = mrb_class(mrb, recv);
      mrb_method_t rt = mrb_method_search_vm(mrb, &owner, rt_id);
      if (!MRB_METHOD_UNDEF_P(rt) && !mrb_func_basic_p(mrb, recv, rt_id, obj_respond_to)) {
        mrb_value args[] = {mrb_symbol_value(rtm_id), mrb_true_value()};
        mrb_int argc = 2;
        if (MRB_METHOD_PROC_P(rt) && mrb_proc_arity(MRB_METHOD_PROC(rt)) == 1) {
          argc = 1;
          mrb_value kernel = mrb_obj_value(mrb->kernel_module);
          if ((mrb->kernel_module->flags & ORUBY_KERNEL_CHILLED_WARNING) &&
              mrb_respond_to(mrb, kernel, MRB_SYM(__oruby_warn_here))) {
            const struct RProc *proc = MRB_METHOD_PROC(rt);
            while (MRB_PROC_ALIAS_P(proc)) proc = proc->upper;
            const char *file = NULL;
            int32_t line = 0;
            if (!MRB_PROC_CFUNC_P(proc)) {
              mrb_debug_get_position(mrb, proc->body.irep, 0, &line, &file);
            }
            mrb_value message = mrb_format(mrb,
              "%v.respond_to?(:respond_to_missing?) uses the deprecated method signature, which takes one parameter\n%s:%d: respond_to? is defined here",
              mrb_obj_as_string(mrb, recv), file ? file : "(unknown)", (int)line);
            mrb_funcall_id(mrb, kernel, MRB_SYM(__oruby_warn_here), 2, message,
                           mrb_symbol_value(MRB_SYM(deprecated)));
          }
        }
        if (!mrb_test(mrb_funcall_argv(mrb, recv, rt_id, argc, args))) return mrb_nil_value();
      }
    C
    metaclass_freeze(source)
  end

  def self.variable(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include \"oruby-vm-compat.h\"")
    replace(source, "  mrb_funcall_argv(mrb, mrb_obj_value(c), MRB_SYM(const_added), 1, &name);", <<~'C'.chomp.gsub(/^/, "  "))
      mrb_value mod = mrb_obj_value(c);
      if (mrb_obj_respond_to(mrb, mrb->module_class, MRB_SYM(__oruby_const_record))) {
        mrb_funcall_argv(mrb, mod, MRB_SYM(__oruby_const_record), 1, &name);
      }
      mrb_funcall_argv(mrb, mod, MRB_SYM(const_added), 1, &name);
    C
    replace(source, "    mrb_funcall_argv(mrb, mod, MRB_SYM(const_added), 1, &name);", <<~'C'.chomp.gsub(/^/, "    "))
      if (mrb_obj_respond_to(mrb, mrb->module_class, MRB_SYM(__oruby_const_record))) {
        mrb_funcall_argv(mrb, mod, MRB_SYM(__oruby_const_record), 1, &name);
      }
      mrb_funcall_argv(mrb, mod, MRB_SYM(const_added), 1, &name);
    C
    replace(source, <<~'C'.chomp.gsub(/^/, "  "), <<~'C'.chomp.gsub(/^/, "  "))
      if (vt) {
        vt->set(mrb, v);
      }
    C
      if (vt) {
        vt->set(mrb, v);
      }
      else if (sym == MRB_GVSYM(VERBOSE)) {
        oruby_chilled_warning_refresh(mrb, v);
      }
    C
    replace(source, '  iv_del(mrb, mrb->globals, sym, NULL);', <<~'C'.chomp.gsub(/^/, "  "))
      iv_del(mrb, mrb->globals, sym, NULL);
      if (sym == MRB_GVSYM(VERBOSE)) oruby_chilled_warning_refresh(mrb, mrb_nil_value());
    C
  end

  def self.implicit_rest(source)
    source.gsub!('#include "../include/', '#include "')
    replace(source, '#include <assert.h>', "#include <assert.h>\n#include <stdatomic.h>\n#define MRC_TARGET_MRUBY 1\n#include <mruby.h>\n#include \"oruby-vm-compat.h\"\n\nstatic _Atomic int oruby_chilled_options;\nvoid oruby_compiler_literal_options(int options) { atomic_store(&oruby_chilled_options, options); }")
    replace(source, '              genop_3(s, OP_SEND, cursp(), new_sym(s, MRC_OPSYM_2(eqq)), 1);', <<~'C'.chomp.gsub(/^/, "              "))
      static const uint8_t case_equal[] = "__oruby_case_equal";
      genop_3(s, OP_SEND, cursp(), new_sym(s, nsym(s->c->p, case_equal, sizeof(case_equal)-1)), 1);
    C
    replace(source, '  genop_3(s, OP_SEND, reg, new_sym(s, MRC_SYM_1(deconstruct)), 0);', <<~'C'.chomp.gsub(/^/, "  "))
      genop_3(s, OP_SEND, reg, new_sym(s, MRC_SYM_1(deconstruct)), 0);
      static const uint8_t deconstructed[] = "__oruby_deconstructed";
      genop_3(s, OP_SEND, reg, new_sym(s, nsym(s->c->p, deconstructed, sizeof(deconstructed)-1)), 0);
    C
    replace(source, '      memcpy(s->irep->lv, prev->irep->lv, lv_size);', <<~'C'.chomp.gsub(/^/, "      "))
      /* Binding reaches the outer names through upvars. The synthetic
       * iteration block uses its registers for the argument and temporaries.
       * Keep unnamed slots for the compiler's local-variable dump format. */
      memset(s->irep->lv, 0, lv_size);
    C
    replace(source, <<~'C'.chomp.gsub(/^/, "        "), <<~'C'.chomp.gsub(/^/, "        "))
      push_n(2); pop_n(2); /* space for one arg and a block */
      pop();
      genop_3(s, OP_SEND, cursp(), new_sym(s, MRC_SYM_1(__case_eqq)), 1);
    C
      push_n(2); pop_n(2); /* space for one arg and a block */
      pop();
      static const uint8_t rescue_match[] = "__oruby_rescue_splat";
      genop_3(s, OP_SEND, cursp(), new_sym(s, nsym(s->c->p, rescue_match, sizeof(rescue_match)-1)), 1);
    C
    replace(source, "        err_begin = s->pc;\n        /* rescue */", <<~'C'.chomp.gsub(/^/, "        "))
      err_begin = s->pc;
      /* Exceptions from evaluating a rescue expression inherit this cause. */
      genop_2(s, OP_SETGV, exc, new_sym(s, MRC_SYM_2(errinfo)));
      /* rescue */
    C
    replace(source, <<~'C'.chomp.gsub(/^/, "      "), <<~'C'.chomp.gsub(/^/, "      "))
      } else {
        /* anonymous rest (*) or implicit rest from a trailing comma (|a,|) */
        pm_constant_id_t astr = MRC_OPSYM_2(mul);
        mrc_constant_id_list_append(s, lv, astr);
    C
      } else {
        /* Retain the destructuring slot and distinguish it for reflection. */
        static const uint8_t implicit[] = "__oruby_implicit_rest";
        pm_constant_id_t astr = nint(parameters->rest) == PM_IMPLICIT_REST_NODE
          ? nsym(s->c->p, implicit, sizeof(implicit) - 1)
          : MRC_OPSYM_2(mul);
        mrc_constant_id_list_append(s, lv, astr);
    C
    replace(source, "        genop_2(s, OP_STRING, cursp(), off);\n        push();\n      }\n      break;\n    }\n    case PM_X_STRING_NODE:", <<~'C'.chomp.gsub(/^(?=.)/, "    "))
          genop_2(s, OP_STRING, cursp(), off);
          int options = atomic_load(&oruby_chilled_options);
          if (!(options & 1) && !(tree->flags & (PM_STRING_FLAGS_MUTABLE | PM_STRING_FLAGS_FROZEN))) {
            genop_3(s, OP_DEBUG, cursp(), ORUBY_CHILLED_DEBUG_TAG, (options >> 1) & 1);
          }
          push();
        }
        break;
      }
      case PM_X_STRING_NODE:
    C
    replace(source, "      if (nt == PM_INTERPOLATED_SYMBOL_NODE) {\n        if (val) {", <<~'C'.chomp.gsub(/^/, "      "))
      if (val && nt == PM_INTERPOLATED_STRING_NODE &&
          !(tree->flags & (PM_INTERPOLATED_STRING_NODE_FLAGS_MUTABLE | PM_INTERPOLATED_STRING_NODE_FLAGS_FROZEN))) {
        int options = atomic_load(&oruby_chilled_options);
        if (!(options & 1)) {
          size_t part = 0;
          while (part < size && nint(nodes[part]) == PM_STRING_NODE) part++;
          if (part == size) genop_3(s, OP_DEBUG, cursp() - 1, ORUBY_CHILLED_DEBUG_TAG, (options >> 1) & 1);
        }
      }
      if (nt == PM_INTERPOLATED_SYMBOL_NODE) {
        if (val) {
    C
    # a variable-like call (no receiver, arguments or parentheses) is the n=0
    # form of OP_SSEND, not OP_SSEND0: go-mrb.c's raise hook tells it from
    # foo() for MRI's NameError "undefined local variable or method"
    replace(source, "    if (!blk && n == 0 && nk == 0) {\n      genop_2(s, OP_SSEND0, cursp(), new_sym(s, sym));\n    }",
      "    if (!blk && n == 0 && nk == 0) {\n" \
      "      if (PM_NODE_TYPE_P(tree, PM_CALL_NODE) && PM_NODE_FLAG_P(tree, PM_CALL_NODE_FLAGS_VARIABLE_CALL))\n" \
      "        genop_3(s, OP_SSEND, cursp(), new_sym(s, sym), 0);\n" \
      "      else\n" \
      "        genop_2(s, OP_SSEND0, cursp(), new_sym(s, sym));\n" \
      "    }")
  end

  def self.chilled_mutation(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include <mruby/hash.h>\n#include \"oruby-vm-compat.h\"")
    replace(source, "MRB_API void\nmrb_check_frozen(mrb_state", <<~'C'.chomp)
      void
      oruby_chilled_warning_refresh(mrb_state *mrb, mrb_value verbose)
      {
        struct RClass *kernel = mrb->kernel_module;
        if (!kernel) return;
        kernel->flags &= ~(ORUBY_KERNEL_CHILLED_WARNING | ORUBY_KERNEL_UNUSED_BLOCK_WARNING);
        if (mrb_nil_p(verbose) || !mrb_class_defined_id(mrb, MRB_SYM(Warning))) return;
        mrb_value warning = mrb_const_get(mrb, mrb_obj_value(mrb->object_class), MRB_SYM(Warning));
        if (!mrb_module_p(warning)) return;
        mrb_value settings = mrb_iv_get(mrb, warning, MRB_IVSYM(settings));
        mrb_value category = mrb_symbol_value(MRB_SYM(deprecated));
        mrb_bool enabled = mrb_hash_p(settings) && mrb_hash_key_p(mrb, settings, category)
          ? mrb_test(mrb_hash_get(mrb, settings, category)) : mrb_test(verbose);
        if (enabled) kernel->flags |= ORUBY_KERNEL_CHILLED_WARNING;
        category = mrb_symbol_value(MRB_SYM(strict_unused_block));
        enabled = mrb_hash_p(settings) && mrb_hash_key_p(mrb, settings, category)
          ? mrb_test(mrb_hash_get(mrb, settings, category)) : mrb_test(verbose);
        if (enabled) kernel->flags |= ORUBY_KERNEL_UNUSED_BLOCK_WARNING;
      }

      void
      oruby_chilled_warn(mrb_state *mrb, struct RBasic *obj)
      {
        mrb_value category = mrb_symbol_value(MRB_SYM(deprecated));
        if (!mrb_obj_respond_to(mrb, mrb_class(mrb, mrb_obj_value(mrb->kernel_module)), MRB_SYM(__oruby_warn_here))) return;
        int arena = mrb_gc_arena_save(mrb);
        mrb_value origin = mrb_iv_get(mrb, mrb_obj_value(obj), MRB_SYM(__oruby_frozen_origin));
        mrb_value message = mrb_string_p(origin)
          ? mrb_format(mrb, "literal string will be frozen in the future\n%v: info: the string was created here", origin)
          : mrb_str_new_lit(mrb, "literal string will be frozen in the future (run with --debug-frozen-string-literal for more information)");
        mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__oruby_warn_here), 2,
                       message, category);
        mrb_gc_arena_restore(mrb, arena);
      }

      MRB_API void
      mrb_check_frozen(mrb_state
    C
    replace(source, "    mrb_frozen_error(mrb, o);\n  }\n}", <<~'C'.chomp.gsub(/^/, ""))
          mrb_frozen_error(mrb, o);
        }
        struct RBasic *obj = o;
        if (mrb_unlikely((obj->flags & (ORUBY_STRING_CHILLED | ORUBY_STRING_TEMP_LOCK)) && obj->tt == MRB_TT_STRING)) {
          if (obj->flags & ORUBY_STRING_TEMP_LOCK) mrb_raise(mrb, E_RUNTIME_ERROR, "can't modify string; temporarily locked");
          oruby_chilled_mutation(mrb, obj);
        }
      }
    C
  end

  def self.chilled_uplus(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include \"oruby-vm-compat.h\"")
    replace(source, "  if (mrb_frozen_p(mrb_obj_ptr(str))) {\n    return str_copy_with_class(mrb, str);", <<~'C'.chomp.gsub(/^/, "  "))
      if (mrb_frozen_p(mrb_obj_ptr(str)) || (mrb_basic_ptr(str)->flags & ORUBY_STRING_CHILLED)) {
        return str_copy_with_class(mrb, str);
    C
  end

  def self.fiber_root_transfer(source)
    replace(source, '#define CINFO_RESUMED 3', "#define CINFO_RESUMED 3\n#define ORUBY_FIBER_TRANSFER_LINK (1u << 1)")
    replace(source, '"resuming transferred fiber"', '"attempt to resume a transferring fiber"')
    replace(source, '  case MRB_FIBER_RUNNING:' + "\n  case MRB_FIBER_RESUMED:", <<~'C'.chomp.gsub(/^/, "  "))
      case MRB_FIBER_SUSPENDED:
        if (!resume) return fiber_error(mrb, "attempt to transfer to a yielding fiber");
        break;
      case MRB_FIBER_RUNNING:
      case MRB_FIBER_RESUMED:
    C
    replace(source, "    c->prev = mrb->c;\n  }\n  else {\n    old_c->status = MRB_FIBER_TRANSFERRED;\n    // c->prev = mrb->root_c;\n    c->prev = NULL;", <<~'C'.chomp.gsub(/^/, "  "))
        c->prev = old_c;
        c->fib->flags &= ~ORUBY_FIBER_TRANSFER_LINK;
      }
      else {
        old_c->status = MRB_FIBER_TRANSFERRED;
        mrb_bool linked = old_c->fib && (old_c->fib->flags & ORUBY_FIBER_TRANSFER_LINK);
        /* An onward transfer inherits the resumed link. Returning to that
           link restores the target's own resumer and avoids a cycle. */
        if (!linked || old_c->prev != c) {
          c->prev = linked ? old_c->prev : (old_c->prev ? old_c : NULL);
          c->fib->flags |= ORUBY_FIBER_TRANSFER_LINK;
        }
    C
    replace(source, '  fiber_switch_context(mrb, c);' + "\n  if (status == MRB_FIBER_CREATED) {", <<~'C'.chomp.gsub(/^/, "  "))
      /* A chain of transfers shares the nested VM that the root entered. */
      if (!resume && !vmexec && old_c->vmexec && !old_c->prev) {
        old_c->vmexec = FALSE;
        c->vmexec = TRUE;
      }
      fiber_switch_context(mrb, c);
      if (status == MRB_FIBER_CREATED) {
    C
    replace(source, '  fiber_check_cfunc_recursive(mrb, mrb->c);', <<~'C'.chomp.gsub(/^/, "  "))
      /* A nested VM can drive a transfer while the root's C frames stay
         on their own stack. Non-root callers still need the normal guard. */
      mrb_bool drive = FALSE;
      if (mrb->c == mrb->root_c) {
        for (mrb_callinfo *ci = mrb->c->ci; ci >= mrb->c->cibase; --ci) {
          if (ci->cci > 0) { drive = TRUE; break; }
        }
      }
      else if (mrb->c->prev && c != mrb->root_c) {
        /* The target will return through this fiber to its resumer. */
        fiber_check_cfunc(mrb, mrb->c);
      }
      else if (c != mrb->root_c) {
        fiber_check_cfunc_recursive(mrb, mrb->c);
      }
    C
    replace(source, "    mrb->c->status = MRB_FIBER_TRANSFERRED;\n    fiber_switch_context(mrb, c);", <<~'C'.chomp.gsub(/^/, "    "))
      struct mrb_context *from = mrb->c;
      struct mrb_context *driver = from;
      while (driver != c && driver->prev) driver = driver->prev;
      if (driver != c && driver->vmexec) {
        driver->vmexec = FALSE;
        c->ci->cci = CINFO_RESUMED;
      }
      else {
        fiber_check_cfunc_recursive(mrb, from);
      }
      from->status = MRB_FIBER_TRANSFERRED;
      fiber_switch_context(mrb, c);
    C
    replace(source, '  return fiber_switch(mrb, self, len, a, FALSE, FALSE);',
      '  return fiber_switch(mrb, self, len, a, FALSE, drive);')
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
    replace(source, <<~'C'.chomp.gsub(/^/, "  "), <<~'C'.chomp.gsub(/^/, "  "))
      if (!mrb_class_p(target) && !mrb_module_p(target)) {
        mrb_raisef(mrb, E_TYPE_ERROR, "wrong argument type %C (expected Class or Module)", mrb_obj_class(mrb, target));
      }
    C
      if (!mrb_class_p(target) && !mrb_module_p(target) && !mrb_sclass_p(target)) {
        mrb_raisef(mrb, E_TYPE_ERROR, "wrong argument type %C (expected Class or Module)", mrb_obj_class(mrb, target));
      }
    C
    # CRuby warns that a module's own ancestors are not imported
    replace(source, "    arg.from = mrb_class_ptr(argv[i]);\n", <<~'C'.gsub(/^/, "    "))
      arg.from = mrb_class_ptr(argv[i]);
      if ((arg.from->super || (arg.from->flags & MRB_FL_CLASS_IS_PREPENDED)) &&
          mrb_respond_to(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__oruby_warn_here))) {
        mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__oruby_warn_here), 1,
          mrb_format(mrb, "%v has ancestors, but Refinement#import_methods doesn't import their methods", argv[i]));
      }
    C
    replace(source, '  struct RProc *np = MRB_OBJ_ALLOC(mrb, MRB_TT_PROC, mrb->proc_class);',
      '  struct RProc *np = MRB_OBJ_ALLOC(mrb, MRB_TT_PROC, mrb_obj_class(mrb, self));')
    replace(source, '  if (MRB_PROC_CFUNC_P(p) || MRB_PROC_ALIAS_P(p) || (MRB_PROC_SCOPE_P(p) && MRB_PROC_STRICT_P(p))) {',
      "  if (MRB_PROC_CFUNC_P(p) || MRB_PROC_ALIAS_P(p) || (MRB_PROC_SCOPE_P(p) && MRB_PROC_STRICT_P(p)) ||\n" \
      "      mrb_iv_defined(mrb, self, MRB_IVSYM(__oruby_method)) ||\n" \
      "      mrb_iv_defined(mrb, self, MRB_IVSYM(__oruby_symproc))) {")
  end

  def self.proc_identity(source)
    replace(source, '  if (ra) return -(min + 1);', <<~'C'.chomp.gsub(/^/, "  "))
      if (ra && (!irep->lv || ma + op >= irep->nlocals - 1 ||
                 irep->lv[ma + op] != MRB_SYM(__oruby_implicit_rest))) return -(min + 1);
    C
    replace(source, '  else if (MRB_PROC_ENV(p1) != MRB_PROC_ENV(p2)) return FALSE;', <<~'C'.chomp.gsub(/^/, "  "))
      else if (MRB_PROC_ENV(p1) != MRB_PROC_ENV(p2)) return FALSE;
      #ifdef MRB_USE_REFINEMENTS
      if (MRB_PROC_REFSCOPE(p1) != MRB_PROC_REFSCOPE(p2) &&
          (mrb_proc_refined_p(mrb, p1) || mrb_proc_refined_p(mrb, p2))) return FALSE;
      #endif
    C
  end

  def self.gc(source)
    replace(source, '#include <mruby/khash.h>', "#include <mruby/khash.h>\n#include \"oruby-alloc-trace.h\"")
    replace(source, "  paint_partial_white(gc, &p->as.basic);\n  return &p->as.basic;", <<~'C'.chomp.gsub(/^/, "  "))
      paint_partial_white(gc, &p->as.basic);
      if (mrb_unlikely(mrb->kernel_module && (mrb->kernel_module->flags & ORUBY_TRACE_ACTIVE))) {
        orb_alloc_record(mrb, &p->as.basic);
      }
      return &p->as.basic;
    C
    replace(source, <<~'C'.chomp, <<~'C'.chomp)
      obj_free(mrb_state *mrb, struct RBasic *obj, mrb_bool end)
      {
        DEBUG(fprintf(stderr, "obj_free(%p,tt=%d)\n",obj,obj->tt));
        if (mrb_gc_giv_p(mrb, obj)) {
    C
      obj_free(mrb_state *mrb, struct RBasic *obj, mrb_bool end)
      {
        DEBUG(fprintf(stderr, "obj_free(%p,tt=%d)\n",obj,obj->tt));
        if (mrb_unlikely(!end && mrb->kernel_module && (mrb->kernel_module->flags & ORUBY_TRACE_HISTORY))) {
          orb_alloc_forget(mrb, obj);
        }
        if (mrb_gc_giv_p(mrb, obj)) {
    C
    replace(source, "  gc->state = MRB_GC_STATE_SWEEP;", <<~'C'.chomp.gsub(/^/, "  "))
      /* Drop weak Proc cache keys before their storage can be recycled. */
      mrb_const_cache_clear(mrb);
      if (mrb->kernel_module && (mrb->kernel_module->flags & (ORUBY_TRACE_ACTIVE | ORUBY_TRACE_HISTORY))) {
        struct orb_alloc_trace *trace = orb_alloc_state(mrb);
        if (trace) ++trace->generation;
      }
      gc->state = MRB_GC_STATE_SWEEP;
    C
  end

  def self.objectspace(source)
    replace(source, '#include <mruby.h>', "#include <mruby.h>\n#include <mruby/presym.h>")
    replace(source, '  struct RClass *target_module;',
      "  struct RClass *target_module;\n  struct RClass *attached_class;")
    replace(source, '  /* filter class kind if target module defined */', <<~'C'.chomp.gsub(/^/, "  "))
      /* A class's singleton filter walks its normal class descendants and
         the visible singleton classes of their instances. */
      if (d->attached_class) {
        struct RClass *klass;
        if (obj->tt == MRB_TT_CLASS) {
          klass = (struct RClass*)obj;
        }
        else if (obj->tt == MRB_TT_SCLASS) {
          mrb_value attached = mrb_iv_get(mrb, mrb_obj_value(obj), MRB_SYM(__attached__));
          if (mrb_nil_p(attached) || mrb_class_p(attached) || mrb_module_p(attached) || mrb_sclass_p(attached)) {
            return MRB_EACH_OBJ_OK;
          }
          klass = mrb_obj_class(mrb, attached);
        }
        else {
          return MRB_EACH_OBJ_OK;
        }
        while (klass && klass != d->attached_class) klass = mrb_class_real(klass->super);
        if (!klass) return MRB_EACH_OBJ_OK;
      }
      else
      /* filter class kind if target module defined */
    C
    replace(source, '  mrb_objspace_each_objects(mrb, os_each_object_cb, &d);', <<~'C'.chomp.gsub(/^/, "  "))
      if (d.target_module && d.target_module->tt == MRB_TT_SCLASS) {
        mrb_value attached = mrb_iv_get(mrb, mrb_obj_value(d.target_module), MRB_SYM(__attached__));
        if (mrb_class_p(attached)) d.attached_class = mrb_class_ptr(attached);
      }
      mrb_objspace_each_objects(mrb, os_each_object_cb, &d);
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
    # a module method bound to a receiver whose ancestry lacks the module:
    # as in CRuby, it runs as if the module came first, so a super goes on
    # from the receiver's class (an ICLASS made as include_class_new makes one)
    replace(source, "    if (ic) tc = ic;\n  }\n", <<~'C'.gsub(/^/, "    "))
      if (ic) tc = ic;
      else {
        struct RClass *bc = MRB_OBJ_ALLOC(mrb, MRB_TT_ICLASS, mrb->class_class);
        bc->mt = tc->mt;
        bc->c = tc;
        bc->super = mrb_class(mrb, recv);
        tc = bc;
      }
    }
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

  def self.regexp_executor(source)
    replace(source, '  for (; !s.nomem && sp <= str_end; sp++) {', <<~'C'.chomp.gsub(/^/, "  "))
      for (; !s.nomem && sp <= str_end; sp++) {
        if (orb_regexp_poll()) break;
    C
    replace(source, '    if (++m->steps > MRB_REGEXP_STEP_LIMIT) return BT_LIMIT;',
      '    if (++m->steps > MRB_REGEXP_STEP_LIMIT || orb_regexp_poll()) return BT_LIMIT;')
    replace(source, '  while (sp + plen <= str_end && sp <= start_cap) {', <<~'C'.chomp.gsub(/^/, "  "))
      while (sp + plen <= str_end && sp <= start_cap) {
        if (orb_regexp_poll()) return RE_OVER_STEP_LIMIT;
    C
  end

  def self.install(build)
    # The regexp fallback uses mruby's Unicode case tables, without loading
    # the mruby Regexp and MatchData classes into ORuby.
    build.cc.defines << "HAVE_MRUBY_REGEXP_GEM"
    output = File.join(build.build_dir, "src")
    overrides = File.join(build.build_dir, "oruby-core")
    inputs = File.join(MRUBY_ROOT, "src")
    {
      "vm" => :vm, "gc" => :gc, "class" => :const_names, "refinement" => :refinement,
      "proc" => :proc_identity, "array" => :array_splat, "variable" => :variable,
      "error" => :chilled_mutation, "kernel" => :defined_queries, "object" => :kind_of_mt
    }.each do |name, patch|
      original = File.join(inputs, "#{name}.c")
      generated = File.join(overrides, "#{name}.c")
      install_source(original, generated, patch)
    end
    build.cc.include_paths << inputs
    build.cc.include_paths << File.expand_path("..", __dir__)
    build.cc.include_paths << File.join(MRUBY_ROOT, "mrbgems/mruby-compiler/lib/prism/include")
    build.cc.include_paths << File.join(MRUBY_ROOT, "build/prism/include")
    build.cc.include_paths << File.join(MRUBY_ROOT, "mrbgems/mruby-compiler/include")
    build.cc.include_paths << File.join(MRUBY_ROOT, "mrbgems/mruby-string-ext/src")
    build.cc.define_rules(output, overrides, build.exts.object)
    build.cc.define_rules(output, overrides, build.exts.presym_preprocessed)

    {
      "mruby-objectspace-weakmap" => ["weakmap.c", :weakmap],
      "mruby-objectspace" => ["mruby_objectspace.c", :objectspace],
      "mruby-compiler" => ["codegen.c", :implicit_rest],
      "mruby-string-ext" => ["string.c", :chilled_uplus],
      "mruby-fiber" => ["fiber.c", :fiber_root_transfer],
      "mruby-method" => ["method.c", :method_lookup],
      "mruby-eval" => ["eval.c", :eval_source],
      "mruby-metaprog" => ["metaprog.c", :metaclass_singleton_methods]
    }.each do |name, (file, patch)|
      relative = File.join("mrbgems", name, "src")
      gem_output = File.join(build.build_dir, relative)
      gem_overrides = File.join(overrides, relative)
      install_source(File.join(MRUBY_ROOT, relative, file), File.join(gem_overrides, file), patch)
      build.cc.define_rules(gem_output, gem_overrides, build.exts.object)
      build.cc.define_rules(gem_output, gem_overrides, build.exts.presym_preprocessed)
    end

    if build.name == "host"
      original = File.join(MRUBY_ROOT, "mrbgems/mruby-regexp/src/re_exec.c")
      install_source(original, File.join(overrides, "regexp/re_exec.c"), :regexp_executor).invoke
    end
  end
end
