# Lists the MRB_API functions declared in mruby's headers that oruby does
# not use: not called as C.name( from a root .go file, nor as name( from the
# cgo shims (go-*.c, go-*.h), which run the raising calls under
# mrb_protect_error. IGNORED names are left out on purpose, with the reason.
oruby_root        = File.join __dir__, '..'
mruby_include_dir = File.join __dir__, '../mruby/include'
files             = Dir["#{mruby_include_dir}/*.h", "#{mruby_include_dir}/mruby/*.h"].sort
# value boxing variants other than the MRB_NO_BOXING oruby builds with
files.reject! { |f| %w[boxing_nan.h boxing_no.h boxing_word.h].include?(File.basename(f)) }

if files.empty?
  $stderr.puts 'Header files not found!'
  exit 1
end

IGNORED = {
  # variadic or va_list: not callable from Go; the shims call them
  'mrb_get_args' => 'varargs; go-mrb.c wraps the specs oruby uses',
  'mrb_get_args_a' => 'varargs array form; GetArgs covers it',
  'mrb_vformat' => 'va_list',
  # Windows locale conversion: Go strings are UTF-8 already
  'mrb_mbs_to_wcs' => 'Windows only', 'mrb_wcs_to_mbs' => 'Windows only',
  'mrb_utf8_from_locale' => 'Windows only', 'mrb_locale_from_utf8' => 'Windows only',
  # mruby internals, or plumbing a Go host does not have
  'mrb_basic_alloc_func' => 'allocator hook',
  'mrb_nan_serial_next' => 'NaN boxing only; oruby uses MRB_NO_BOXING',
  'mrb_read_int' => 'C string parsing; Go strconv', 'mrb_int_read' => 'C string parsing; Go strconv',
  'mrb_read_float' => 'C string parsing; Go strconv', 'mrb_int_to_cstr' => 'C buffer formatting',
  'mrb_stream_is_unreadable' => 'takes a C FILE*',
  'mrb_vm_ci_env_clear' => 'VM internal', 'mrb_ary_make_shared_copy' => 'array internal',
  'mrb_range_ptr' => 'struct access; range.go reads ranges',
  'mrb_temp_alloc' => 'C scratch memory', 'mrb_gc_add_region' => 'static heap regions (MCU)',
  'mrb_gc_scheduler_jitter' => 'mruby task scheduler bookkeeping',
  'mrb_method_cache_clear' => 'oruby changes method tables through the API only',
  'mrb_const_cache_clear' => 'oruby changes constants through the API only',
  'mrb_fiber_new' => 'Fiber.new from Ruby covers it',
  'mrb_obj_itself' => 'trivial',
  'mrb_c_stack_limit_set' => 'needs MRB_USE_C_STACK_CHECK; MRB_CALL_LEVEL_MAX already stops deep C nesting',
  'mrb_c_stack_check' => 'needs MRB_USE_C_STACK_CHECK',
  'mrb_load_exec' => 'parser-state loading; the LoadString/LoadFile family covers it',
  'mrb_load_nstring' => 'the _cxt variants are used',
  'mrb_ccontext_partial_hook' => 'the Prism compiler stores the hook and never calls it',
  'mrb_check_frozen' => 'pointer form of mrb_check_frozen_value (CheckFrozen)',
  # covered by Go-side equivalents
  'mrb_clear_error' => 'ExcClear', 'mrb_check_error' => 'Exc / ExcClear',
  'mrb_sys_fail' => 'SysFail builds the Errno error in Go',
  'mrb_ensure' => 'Go calls are protected and return error',
  'mrb_rescue' => 'Go calls are protected and return error',
  'mrb_rescue_exceptions' => 'Go calls are protected and return error',
  'mrb_block_given_p' => 'GetArgsBlock',
  'mrb_define_module_function' => 'DefineModuleFunction goes through go-mrb.h proc trampolines',
  'mrb_define_private_method_id' => 'go-mrb.c defines private methods by name',
}.freeze

used = Dir["#{oruby_root}/*.go"].map { |f| File.read(f, encoding: "UTF-8").scan(/\bC\.(\w+)\(/) }.flatten
used += Dir["#{oruby_root}/go-*.{c,h}"].map { |f| File.read(f, encoding: "UTF-8").scan(/\b(mrb_\w+|mempool_\w+)\s*\(/) }.flatten
used = used.to_h { |n| [n, true] }

missing = 0
files.each do |f|
  names = File.readlines(f, encoding: "UTF-8").flat_map { |line| line.scan(/^MRB_API(?:.*?)\s*([A-Za-z_]\w*)\(/).flatten }
  names = names.uniq.reject { |n| used[n] || IGNORED.key?(n) }
  next if names.empty?

  puts "\n--- API not used by oruby, from #{File.basename(f)}: ---"
  names.each { |n| puts n }
  missing += names.size
end
puts "\n#{missing} MRB_API functions not used (#{IGNORED.size} more ignored on purpose, see IGNORED)"
