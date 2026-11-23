module Kernel
  def p(*objects)
    objects.each do |object|
      file = ObjectSpace.allocation_sourcefile(object)
      line = ObjectSpace.allocation_sourceline(object)
      if file
        $stdout.puts "#{object.inspect} @ #{file}:#{line}"
      else
        $stdout.puts object.inspect
      end
    end
  end
  private :p
end

ObjectSpace.trace_object_allocations_start
warn "objspace/trace is enabled"
