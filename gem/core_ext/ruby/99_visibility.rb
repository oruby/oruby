# MRI's Kernel module functions: a private instance method and a public
# singleton method each. Runs last, after the files above defined them.
module Kernel
  %i[Array Complex Float Hash Integer Rational String __callee__ __dir__ __method__ ` abort
     at_exit autoload autoload? autoload_relative binding block_given? caller caller_locations
     catch eval exec exit exit! fail fork format gets global_variables iterator? lambda load
     local_variables loop open p pp print printf proc putc puts raise rand readline readlines
     require require_relative select set_trace_func sleep spawn sprintf srand syscall system
     test throw trace_var trap untrace_var warn].each do |m|
    next unless method_defined?(m) || private_method_defined?(m)
    module_function m unless singleton_class.method_defined?(m) || singleton_class.private_method_defined?(m)
    private m
    singleton_class.__send__(:public, m)
  end
end
