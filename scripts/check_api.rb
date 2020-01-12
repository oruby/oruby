mruby_include_dir = 'vendor/mruby/include'
files             = Dir["#{mruby_include_dir}/*.h", "#{mruby_include_dir}/mruby/*.h"]

if files.empty?
  $stderr.puts 'Header files not found!'
  exit 1
end

gos = IO.read('oruby.go')

line_no = 0
header_printed = false

File.readlines('mruby.def').each do |line|

  # Skip first two lines (cpmment and EXPORTS)
  line_no += 1
  next if line_no < 3

  l = line[/([A-Za-z_][\w]*)\b/]
  next if gos.include? "C.#{l}("

  unless header_printed
    puts "\n\r--- API missing in oruby: ---\n\r"
    header_printed = true
  end

  puts line
end
