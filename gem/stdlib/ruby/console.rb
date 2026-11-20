# io/console: the console methods of IO, and StringIO's getch/getpass.
# A terminal's raw and no-echo modes are not modelled: the block runs with
# the stream as it is.
class IO
  def getch(min: nil, time: nil, intr: nil) = getc

  def getpass(prompt = nil)
    $stderr.print(prompt) if prompt
    line = gets
    $stderr.puts
    line&.chomp
  end

  def raw(min: nil, time: nil, intr: nil)
    yield self
  end

  def noecho
    yield self
  end

  def cooked
    yield self
  end

  def echo=(flag)
    flag
  end

  def echo? = true

  def winsize = [24, 80]

  def winsize=(size)
    size
  end

  def console_mode = nil

  def self.console(*)
    @__oruby_console ||= (File.open("/dev/tty", "r+") rescue nil)
  end
end

class StringIO
  def getch(*) = getc

  def getpass(prompt = nil)
    line = gets
    line&.chomp
  end
end
