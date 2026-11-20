# MRI's GC.stat, GC.count and GC.config over mruby's collector. mruby counts
# no collections, so count and major_gc_count are the GC.start runs; every
# value is an Integer, as MRI's (rb_gc_stat answers size_t). The settings
# mruby has no use for (stress, auto_compact, measure_total_time) are kept
# and answered as MRI does.
module GC
  @__oruby_count = 0
  @__oruby_total_ns = 0
  @__oruby_stress = false
  @__oruby_auto_compact = false
  @__oruby_measure = true

  class << self
    alias_method :__oruby_stat, :stat
    alias_method :__oruby_start, :start

    # full_mark:, immediate_mark:, immediate_sweep:: mruby's collection is
    # always a full one
    def start(full_mark: true, immediate_mark: true, immediate_sweep: true)
      @__oruby_count += 1
      if @__oruby_measure && defined?(Process::CLOCK_MONOTONIC)
        t = Process.clock_gettime(Process::CLOCK_MONOTONIC, :nanosecond)
        __oruby_start
        @__oruby_total_ns += Process.clock_gettime(Process::CLOCK_MONOTONIC, :nanosecond) - t
      else
        __oruby_start
      end
      nil
    end

    def count = @__oruby_count

    # nanoseconds spent in GC.start, while measure_total_time is on
    def total_time = @__oruby_total_ns

    def measure_total_time = @__oruby_measure
    def measure_total_time=(flag)
      @__oruby_measure = flag ? true : false
    end

    def stress = @__oruby_stress
    def stress=(flag)
      @__oruby_stress = (Integer === flag) ? flag : (flag ? true : false)
    end

    def auto_compact = @__oruby_auto_compact
    def auto_compact=(flag)
      @__oruby_auto_compact = flag ? true : false
    end

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

module GC
  # GC#garbage_collect, as MRI's: GC.start for an object extended with GC
  def garbage_collect(full_mark: true, immediate_mark: true, immediate_sweep: true)
    GC.start
  end

  # MRI's profiler records each collection; mruby reports none, so the
  # profile is empty: enable/disable are kept, result and report are empty
  module Profiler
    @enabled = false

    class << self
      def enabled? = @enabled

      def enable
        @enabled = true
        nil
      end

      def disable
        @enabled = false
        nil
      end

      def clear = nil
      def raw_data = []
      def result = +""
      def total_time = 0.0

      def report(out = $stdout)
        out.write(result)
        nil
      end
    end
  end
end

module ObjectSpace
  def self.garbage_collect(full_mark: true, immediate_mark: true, immediate_sweep: true)
    GC.start
  end
end
