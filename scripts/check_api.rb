oruby_root = File.join __dir__, '..'
mruby_include_dir = File.join __dir__, '../mruby/include'
files             = Dir["#{mruby_include_dir}/*.h", "#{mruby_include_dir}/mruby/*.h"]

if files.empty?
  $stderr.puts 'Header files not found!'
  exit 1
end

header_printed = false

files.each do |f|
  next if FileTest.directory?(f)
  next if ["boxing_nan.h", "boxing_no.h", "boxing_word.h"].include?(File.basename(f))

  header_printed = false

  gos = nil

  File.readlines(f).each do |line|
    l = line.scan(/^MRB_API(?:.*?)\s*([A-Za-z_][\w]*)\(/)
    next if l.nil? || l.empty?
  
    gofile = "#{oruby_root}/#{File.basename(f,'.*')}.go"
    
    if File.file?(gofile)
      gos = IO.read(gofile) unless gos
      next if gos.include? "C.#{l[0][0]}("

      if File.basename(f,'.*') == 'mruby'
        alt = IO.read("#{oruby_root}/class.go")
        next if alt.include? "C.#{l[0][0]}("
      end
    end

    unless header_printed
      puts "\n\r--- API missing in #{File.basename(f,'.*')}.go:#{' (FILE DOES NOT EXIST)' unless gos} ---\n\r"
      header_printed = true
    end
  
    puts l
  end
end


