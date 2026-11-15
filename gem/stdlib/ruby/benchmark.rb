module Benchmark
  class Tms
    attr_reader :utime, :stime, :cutime, :cstime, :real

    def initialize(utime = 0.0, stime = 0.0, cutime = 0.0, cstime = 0.0, real = 0.0)
      @utime = utime
      @stime = stime
      @cutime = cutime
      @cstime = cstime
      @real = real
    end

    def total
      @utime + @stime
    end
  end

  def self.measure
    start = __clock
    yield
    Tms.new(0.0, 0.0, 0.0, 0.0, __clock - start)
  end

  def self.realtime
    start = __clock
    yield
    __clock - start
  end
end