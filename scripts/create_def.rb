ctags_exe         = 'ctags'
mruby_include_dir = 'vendor/mruby/include'
files             = Dir["#{mruby_include_dir}/*.h", "#{mruby_include_dir}/mruby/*.h"]

if files.empty?
  $stderr.puts 'Header files not found!'
  exit 1
end

gos = IO.read('./oruby.go')

File.open('mruby.def', 'w') do |f|
  f.puts 'LIBRARY mruby.dll'
  f.puts 'EXPORTS'

  IO.popen("#{ctags_exe} -u -x --c-kinds=p #{files.join(' ')}") do |io|
    io.each_line do |line|
      l = line[/^([A-Za-z_][\w]*)\b/]

      unless %w(mrb_float_pool mrb_float_value mrb_voidp_value mrb_cptr_value mrb_exc_print).include?(l)
        f.putc "\t"
        f.puts l

        # print tag line on screen if it isn't Go implemented
        puts line unless gos.include? "C.#{l}("
      end
    end
  end

  # append manual exports
  f.puts "\tmrb_digitmap\tDATA\n"
end
