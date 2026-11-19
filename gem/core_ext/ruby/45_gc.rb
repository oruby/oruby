# MRI's GC.stat, GC.count and GC.config over mruby's collector. mruby counts
# no collections, so count and major_gc_count are the GC.start runs; every
# value is an Integer, as MRI's (rb_gc_stat answers size_t).
module GC
  @__oruby_count = 0

  class << self
    alias_method :__oruby_stat, :stat
    alias_method :__oruby_start, :start

    def start(...)
      @__oruby_count += 1
      __oruby_start(...)
    end

    def count = @__oruby_count

    def stat(arg = nil)
      unless arg.nil? || Symbol === arg || Hash === arg
        raise TypeError, "non-hash or symbol given"
      end
      h = __oruby_stat_hash
      if Symbol === arg
        raise ArgumentError, "unknown key: #{arg}" unless h.key?(arg)
        return h[arg]
      end
      return h if arg.nil?
      h.each { |k, v| arg[k] = v }
      arg
    end

    def __oruby_stat_hash
      objs = ObjectSpace.count_objects
      h = {
        count: @__oruby_count,
        major_gc_count: @__oruby_count,
        minor_gc_count: 0,
        heap_live_slots: objs[:TOTAL] - objs[:FREE],
        heap_free_slots: objs[:FREE],
        heap_available_slots: objs[:TOTAL],
        total_allocated_objects: objs[:TOTAL] - objs[:FREE]
      }
      __oruby_stat.each do |k, v|
        next if h.key?(k)
        h[k] = case v
               when Integer then v
               when true then 1
               when false, nil then 0
               when Float then v.to_i
               else v.hash & 0xffff
               end
      end
      h
    end
    private :__oruby_stat_hash

    # mruby's one setting: generational mode
    def config(arg = (no_arg = true; nil))
      unless no_arg || arg.nil?
        raise ArgumentError, "invalid argument" unless Hash === arg
        if arg.key?(:implementation)
          raise ArgumentError, 'Attempting to set read-only key "Implementation"'
        end
        self.generational_mode = arg[:generational] ? true : false if arg.key?(:generational)
      end
      { implementation: "mruby", generational: generational_mode ? true : false }
    end
  end
end
