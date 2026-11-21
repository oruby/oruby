#ifndef ORUBY_VM_COMPAT_H
#define ORUBY_VM_COMPAT_H

/* A runtime flag for writable IREPs. Aliases and Proc copies share it. */
#define ORUBY_IREP_RUBY2_KEYWORDS 8

/* A VM argument accumulator made by a splat, including bare super's rest. */
#define ORUBY_ARRAY_SPLAT (1u << 13)

#endif
