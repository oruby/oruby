class IO
  def raw(min: nil, time: nil, intr: nil)
    saved = console_mode
    raw!(min: min, time: time, intr: intr)
    yield self
  ensure
    self.console_mode = saved if saved
  end

  def cooked
    saved = console_mode
    cooked!
    yield self
  ensure
    self.console_mode = saved if saved
  end

  def noecho
    saved = console_mode
    self.echo = false
    yield self
  ensure
    self.console_mode = saved if saved
  end

  def getch(min: nil, time: nil, intr: nil)
    if tty?
      raw(min: min, time: time, intr: intr) { getc }
    else
      getc
    end
  end

  def getpass(prompt = nil)
    if prompt
      $stderr.print(prompt)
      $stderr.flush if $stderr.respond_to?(:flush)
    end
    line =
      if tty?
        noecho { gets }
      else
        gets
      end
    $stderr.puts
    line&.chomp
  end

  def beep
    write("\a")
    self
  end

  def goto(row, col)
    write("\e[#{row + 1};#{col + 1}H")
    self
  end

  def goto_column(col)
    write("\e[#{col + 1}G")
    self
  end

  def cursor_up(n = 1)
    write("\e[#{n}A") if n > 0
    self
  end

  def cursor_down(n = 1)
    write("\e[#{n}B") if n > 0
    self
  end

  def cursor_right(n = 1)
    write("\e[#{n}C") if n > 0
    self
  end

  def cursor_left(n = 1)
    write("\e[#{n}D") if n > 0
    self
  end

  def clear_screen
    write("\e[2J\e[H")
    self
  end

  def erase_line(mode = 0)
    write("\e[#{mode}K")
    self
  end

  def erase_screen(mode = 0)
    write("\e[#{mode}J")
    self
  end

  def scroll_forward(n = 1)
    write("\e[#{n}S") if n > 0
    self
  end

  def scroll_backward(n = 1)
    write("\e[#{n}T") if n > 0
    self
  end

  def hide_cursor
    write("\e[?25l")
    self
  end

  def show_cursor
    write("\e[?25h")
    self
  end

  def check_winsize_changed
    prev = winsize
    yield self
    cur = winsize
    cur != prev
  end

  def self.default_console_size
    [24, 80]
  end

  def self.console_size
    con = console
    return con.winsize if con
    default_console_size
  rescue
    default_console_size
  end

  def self.console(*args)
    if args.first == :close
      if @__oruby_console
        @__oruby_console.close unless @__oruby_console.closed?
        @__oruby_console = nil
      end
      return nil
    end
    if @__oruby_console && !@__oruby_console.closed?
      return @__oruby_console
    end
    tty_path = RUBY_PLATFORM.include?("mswin") || RUBY_PLATFORM.include?("mingw") ? "CONIN$" : "/dev/tty"
    @__oruby_console = File.open(tty_path, "r+")
  rescue
    nil
  end
end

class StringIO
  def getch(*)
    getc
  end

  def getpass(prompt = nil)
    if prompt
      $stderr.print(prompt)
      $stderr.flush if $stderr.respond_to?(:flush)
    end
    line = gets
    $stderr.puts
    line&.chomp
  end
end
