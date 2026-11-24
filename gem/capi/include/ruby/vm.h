/* ORuby C API: the VM handle of ruby/vm.h. There is one VM (the C API's
 * mrb_state); a function given to ruby_vm_at_exit runs when it closes. */
#ifndef ORUBY_CAPI_VM_H
#define ORUBY_CAPI_VM_H 1
#include "ruby/ruby.h"
typedef struct ruby_vm_struct ruby_vm_t;
void ruby_vm_at_exit(void (*func)(ruby_vm_t *));
int ruby_vm_destruct(ruby_vm_t *vm);
#endif
