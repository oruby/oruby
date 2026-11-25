require_relative "mruby_hash_keys"

oruby_build = MRuby::Build.new do |conf|
  # load specific toolchain settings

  # Gets set by the VS command prompts.
  #if ENV['VisualStudioVersion']
  #  toolchain :visualcpp
  #else
    toolchain :gcc
  #end

  # enable_debug

  # Use mrbgems
  # conf.gem 'examples/mrbgems/ruby_extension_example'
  # conf.gem 'examples/mrbgems/c_extension_example' do |g|
  #   g.cc.flags << '-g' # append cflags in this gem
  # end
  # conf.gem 'examples/mrbgems/c_and_ruby_extension_example'
  # conf.gem :github => 'masuidrive/mrbgems-example', :branch => 'master'
  # conf.gem :git => 'git@github.com:masuidrive/mrbgems-example.git', :branch => 'master', :options => '-v'

  # include the default GEMs
  conf.gembox 'default'
  conf.gem :core => 'mruby-objectspace-weakmap'

  # the symbol names oruby's C and Go code look up, as presyms (tools/gensym
  # writes its source; last, so the numbers of the other gems' stay)
  conf.gem __dir__ + "/mruby-oruby-presym"

  # C compiler settings
  # conf.cc do |cc|
  #   cc.command = ENV['CC'] || 'gcc'
  #   cc.flags = [ENV['CFLAGS'] || %w()]
  #  removed -O3, added -g -gstabs+
    cc.flags = [%w(-g -ggdb -mthreads -O0 -Wall -Werror-implicit-function-declaration -gstabs+)] #=[ENV['CFLAGS'] || %w()]
  #   cc.include_paths = ["#{root}/include"]
  #   cc.defines = %w(DISABLE_GEMS)
  cc.defines << %w(MRB_HIGH_PROFILE MRB_METHOD_T_STRUCT MRB_NO_BOXING _hypot=hypot MRB_ARY_LENGTH_MAX=0)
  # Prism's allocations come from libc: mruby-compiler otherwise routes them
  # through one process-wide mrb_state (global_mrb), so states compiling on
  # different threads at once run each other's GC
  cc.defines << 'MRC_PRISM_LIBC'
  # Nesting as deep as MRI's code takes (Prism's 10000 there): mruby caps
  # both at 256, for a small C stack. Go threads have 8 MB, where an -O3
  # build compiles 8000-deep literals without overflowing; 2048 keeps a 4x
  # margin (deeper code is a SyntaxError, not a crash)
  cc.defines += %w(PRISM_DEPTH_MAXIMUM=2048 MRC_CODEGEN_LEVEL_MAX=2048)
  #   cc.option_include_path = '-I%s'
  #   cc.option_define = '-D%s'
  #   cc.compile_options = "%{flags} -MMD -o %{outfile} -c %{infile}"
  # end

  # mrbc settings
  conf.mrbc do |mrbc|
    mrbc.compile_options = "-g -B%{funcname} -o-" # The -g option is required for line numbers
  end

  # Linker settings
  # conf.linker do |linker|
  #   linker.command = ENV['LD'] || 'gcc'
  #   linker.flags = [ENV['LDFLAGS'] || []]
  #   linker.flags_before_libraries = []
  #   linker.libraries = %w()
  #   linker.flags_after_libraries = []
  #   linker.library_paths = []
  #   linker.option_library = '-l%s'
  #   linker.option_library_path = '-L%s'
  #   linker.link_options = "%{flags} -o %{outfile} %{objs} %{libs}"
  # end

  # Archiver settings
  # conf.archiver do |archiver|
  #   archiver.command = ENV['AR'] || 'ar'
  #   archiver.archive_options = 'rs %{outfile} %{objs}'
  # end

  # Parser generator settings
  # conf.yacc do |yacc|
  #   yacc.command = ENV['YACC'] || 'bison'
  #   yacc.compile_options = '-o %{outfile} %{infile}'
  # end

  # gperf settings
  # conf.gperf do |gperf|
  #   gperf.command = 'gperf'
  #   gperf.compile_options = '-L ANSI-C -C -p -j1 -i 1 -g -o -t -N mrb_reserved_word -k"1,3,$" %{infile} > %{outfile}'
  # end

  # file extensions
  # conf.exts do |exts|
  #   exts.object = '.o'
  #   exts.executable = '' # '.exe' if Windows
  #   exts.library = '.a'
  # end

  # file separetor
  # conf.file_separator = '/'
end

oruby_hash_key_rules(oruby_build)

# Define cross build settings
# MRuby::CrossBuild.new('32bit') do |conf|
#   toolchain :gcc
#
#   conf.cc.flags << "-m32"
#   conf.linker.flags << "-m32"
#
#   conf.build_mrbtest_lib_only
#
#   conf.gem 'examples/mrbgems/c_and_ruby_extension_example'
#
#   conf.test_runner.command = 'env'
#
# end
