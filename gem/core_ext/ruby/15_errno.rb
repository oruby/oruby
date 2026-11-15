# mruby-errno builds "unknown error - msg" for Errno::EXXX.new(msg) because
# the class errno is not passed on; build the strerror message here.
class SystemCallError
  alias_method :__sce_initialize, :initialize
  private :__sce_initialize

  def initialize(*args)
    if instance_of?(SystemCallError) || !self.class.const_defined?(:Errno)
      return __sce_initialize(*args)
    end
    eno = self.class::Errno
    return __sce_initialize(*args) unless eno.is_a?(Integer) && eno > 0
    m = args[0]
    str = SystemCallError.new(nil, eno).message
    str = "#{str} - #{m}" unless m.nil?
    Exception.instance_method(:initialize).bind_call(self, str)
  end
end
