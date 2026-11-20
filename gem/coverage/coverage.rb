# MRI's Coverage (ext/coverage/coverage.c). The tables of each file
# instrumented as it loaded (Coverage.__oruby_instrument, called by require
# and load while coverage is set up); the instrumented code counts through
# __oruby_hit, __oruby_c, __oruby_n and __oruby_md.
module Coverage
  @state = :idle
  @mode = nil   # nil: compatible mode (a file's result is its lines Array)
  @files = {}   # path => the file's tables (the latest load of it)
  @by_id = []   # file id => tables; ids grow, so old code never counts into new tables

  class << self
    def supported?(mode)
      unless Symbol === mode
        raise TypeError, "wrong argument type #{mode.class} (expected Symbol)"
      end
      %i[lines oneshot_lines branches methods eval].include?(mode)
    end

    def setup(opt = (compat = true; nil))
      raise RuntimeError, "coverage measurement is already setup" unless @state == :idle
      mode = {}
      if compat
        mode = nil
      elsif opt == :all
        mode = { lines: true, branches: true, methods: true, eval: true }
      else
        unless Hash === opt
          raise TypeError, "no implicit conversion of #{opt.class} into Hash" unless opt.respond_to?(:to_hash)
          opt = opt.to_hash
        end
        mode[:lines] = true if opt[:lines]
        mode[:branches] = true if opt[:branches]
        mode[:methods] = true if opt[:methods]
        if opt[:oneshot_lines]
          raise RuntimeError, "cannot enable lines and oneshot_lines simultaneously" if mode[:lines]
          mode[:oneshot_lines] = true
        end
        mode[:eval] = true if opt[:eval]
        # no target at all: the compatible mode, as MRI's
        mode = nil if mode.empty?
      end
      @mode = mode
      @files = {}
      @state = :suspended
      nil
    end

    def start(*args)
      setup(*args)
      resume
      nil
    end

    def resume
      raise RuntimeError, "coverage measurement is not set up yet" if @state == :idle
      raise RuntimeError, "coverage measurement is already running" if @state == :running
      @state = :running
      nil
    end

    def suspend
      raise RuntimeError, "coverage measurement is not running" unless @state == :running
      @state = :suspended
      nil
    end

    def state = @state

    def running? = @state == :running

    def peek_result
      raise RuntimeError, "coverage measurement is not enabled" if @state == :idle
      h = {}
      @files.each { |path, f| h[path] = __oruby_result_of(f) }
      h.freeze
    end

    def result(opt = (none = true; nil))
      raise RuntimeError, "coverage measurement is not enabled" if @state == :idle
      stop = clear = true
      unless none
        opt = opt.to_hash unless Hash === opt
        stop = opt[:stop] ? true : false
        clear = opt[:clear] ? true : false
      end
      r = peek_result
      if stop && !clear
        warn "stop implies clear", uplevel: 0
        clear = true
      end
      @files.each_value { |f| __oruby_clear(f) } if clear
      if stop
        @state = :idle
        @files = {}
        @mode = nil
      end
      r
    end

    def line_stub(file)
      __oruby_stub(__oruby_read(file))
    end

    # require/load: the source of path with its counters, or nil when
    # coverage is not set up
    def __oruby_instrument(path)
      return nil if @state == :idle
      src = __oruby_read(path)
      mode = @mode || { lines: true }
      f = __oruby_new_file(path)
      code, lines, bases, methods = __oruby_scan(src, f[:fid], mode[:branches] ? true : false,
                                                 mode[:methods] ? true : false, true, 0, 0, 0)
      __oruby_merge(f, lines, bases, methods, 0)
      code
    end

    # eval(src, binding, file, line): with eval: true its lines and branches
    # count into file's tables (made for it when file was not loaded); the
    # methods it defines in a measured file count either way, as MRI's
    def __oruby_eval(src, file, line)
      return nil if @state == :idle || !(String === file) || !(Integer === line)
      mode = @mode || { lines: true }
      f = @files[file]
      evals = mode[:eval] ? true : false
      return nil unless evals || (f && mode[:methods])
      f ||= __oruby_new_file(file)
      off = line > 0 ? line - 1 : 0
      code, lines, bases, methods = __oruby_scan(src, f[:fid], evals && mode[:branches] ? true : false,
                                                 mode[:methods] ? true : false, evals, off, f[:counters].size, f[:methods].size)
      __oruby_merge(f, evals ? lines : [], bases, methods, off)
      code
    end

    def __oruby_hit(fid, line, inc, dec, m, recv = nil)
      return nil unless @state == :running && (f = @by_id[fid])
      if line >= 0 && (lines = f[:lines])
        if (once = f[:oneshot])
          if lines[line] == 0
            lines[line] = 1
            once << line + 1
          end
        elsif lines[line]
          lines[line] += 1
        end
      end
      f[:counters][inc] += 1 if inc >= 0
      f[:counters][dec] -= 1 if dec >= 0
      __oruby_called(f, m, recv) if m >= 0
      nil
    end

    # a call of method m on recv: counted for the owner of the copy that ran
    # (module_function and define_method make copies MRI counts apart)
    def __oruby_called(f, m, recv)
      owners = (f[:owners][m] ||= [])
      entry = owners.find { |o, _| o === recv }
      unless entry
        name = f[:methods][m][0]
        owner = begin
                  recv.singleton_class.instance_method(name).owner
                rescue NameError, TypeError
                  Module === recv ? recv.singleton_class : recv.class
                end
        entry = owners.find { |o, _| o.equal?(owner) }
        owners << (entry = [owner, 0]) unless entry
      end
      entry[1] += 1
    end

    # a predicate: its branch counted by truthiness
    def __oruby_c(fid, t, e, v)
      if @state == :running && (f = @by_id[fid])
        f[:counters][v ? t : e] += 1
      end
      v
    end

    # a &. receiver: then unless nil
    def __oruby_n(fid, t, e, v)
      if @state == :running && (f = @by_id[fid])
        f[:counters][v.nil? ? e : t] += 1
      end
      v
    end

    # a def ran: its method is in the methods table from now on (MRI's
    # me_set, which clear keeps); flavor 0: def m, 1: def self.m, 2: def obj.m
    def __oruby_md(fid, m, flavor, recv, name)
      if (f = @by_id[fid])
        owner = if flavor == 0
                  Module === recv ? recv : recv.class
                else
                  recv.singleton_class
                end
        owners = (f[:owners][m] ||= [])
        owners << [owner, 0] unless owners.any? { |o, _| o.equal?(owner) }
      end
      name
    end

    private

    def __oruby_new_file(path)
      mode = @mode || { lines: true }
      f = {
        fid: @by_id.size,
        lines: (mode[:lines] || mode[:oneshot_lines]) ? [] : nil,
        oneshot: mode[:oneshot_lines] ? [] : nil,
        bases: [],
        counters: [],
        methods: [],
        owners: {},              # method id => [[owner, calls], ...], once defined
      }
      @by_id << f
      @files[path] = f
    end

    # a scan's tables into f; lines counted from line off
    def __oruby_merge(f, lines, bases, methods, off)
      if (fl = f[:lines])
        lines.each_with_index do |v, i|
          j = off + i
          fl << nil while fl.size <= j
          fl[j] ||= 0 if v
        end
      end
      bases.each { |b| f[:counters].concat(Array.new(b[5].size, 0)) }
      f[:bases].concat(bases)
      f[:methods].concat(methods)
    end

    def __oruby_result_of(f)
      return (f[:lines] || []).dup.freeze if @mode.nil?
      h = {}
      if f[:oneshot]
        h[:oneshot_lines] = f[:oneshot].dup.freeze
      elsif f[:lines]
        h[:lines] = f[:lines].dup.freeze
      end
      if @mode[:branches]
        branches = {}
        id = 0
        f[:bases].each do |type, l1, c1, l2, c2, targets|
          children = {}
          key = [type, id, l1, c1, l2, c2].freeze
          id += 1
          targets.each do |label, tl1, tc1, tl2, tc2, ctr|
            children[[label, id, tl1, tc1, tl2, tc2].freeze] = f[:counters][ctr]
            id += 1
          end
          branches[key] = children
        end
        h[:branches] = branches
      end
      if @mode[:methods]
        methods = {}
        f[:owners].each do |m, owners|
          name, l1, c1, l2, c2 = f[:methods][m]
          owners.each { |owner, calls| methods[[owner, name, l1, c1, l2, c2].freeze] = calls }
        end
        h[:methods] = methods
      end
      h
    end

    def __oruby_clear(f)
      if (lines = f[:lines])
        lines.each_index { |i| lines[i] = 0 if lines[i] }
      end
      f[:oneshot]&.clear
      f[:counters].fill(0)
      f[:owners].each_value { |owners| owners.each { |o| o[1] = 0 } }
    end
  end
end
