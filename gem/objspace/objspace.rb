module ObjectSpace
  class << self
    def memsize_of_all(klass = nil)
      total = 0
      if klass
        each_object(klass) { |o| total += memsize_of(o) }
      else
        each_object { |o| total += memsize_of(o) }
      end
      total
    end

    def count_objects_size(result_hash = nil)
      h = result_hash || {}
      h.clear
      total = 0
      each_object do |o|
        sz = memsize_of(o)
        next if sz == 0
        t = __type_sym(o)
        h[t] = (h[t] || 0) + sz
        total += sz
      end
      h[:TOTAL] = total
      h
    end

    def count_tdata_objects(result_hash = nil)
      h = result_hash || {}
      h.clear
      each_object do |o|
        next unless __is_cdata(o)
        k = internal_class_of(o)
        h[k] = (h[k] || 0) + 1
      end
      h
    end

    def count_imemo_objects(result_hash = nil)
      h = result_hash || {}
      h.clear
      h
    end

    def count_nodes(result_hash = nil)
      h = result_hash || {}
      h.clear
      h
    end

    def count_symbols(result_hash = nil)
      h = result_hash || {}
      h.clear
      syms = Symbol.all_symbols
      h[:mortal_dynamic_symbol] = 0
      h[:immortal_dynamic_symbol] = 0
      h[:immortal_static_symbol] = syms.length
      h[:immortal_symbol] = syms.length
      h
    end

    def reachable_objects_from(obj)
      return nil if __is_immediate(obj)

      refs = []
      cls = internal_class_of(obj)
      refs << cls if cls && !__is_immediate(cls)

      case obj
      when (defined?(Thread::Queue) && Thread::Queue)
        obj.__oruby_items.each { |e| refs << e unless __is_immediate(e) }
      when Array
        obj.each { |e| refs << e unless __is_immediate(e) }
      when Hash
        obj.each_pair do |k, v|
          refs << k unless __is_immediate(k)
          refs << v unless __is_immediate(v)
        end
      when Range
        refs << obj.begin unless __is_immediate(obj.begin)
        refs << obj.end unless __is_immediate(obj.end)
      when Struct
        obj.each { |v| refs << v unless __is_immediate(v) }
      end

      if obj.is_a?(Module)
        obj.constants(false).each do |c|
          begin
            v = obj.const_get(c, false)
            refs << v unless __is_immediate(v)
          rescue Exception
          end
        end
      end

      if obj.respond_to?(:instance_variables)
        obj.instance_variables.each do |iv|
          v = obj.instance_variable_get(iv)
          refs << v unless __is_immediate(v)
        end
      end

      refs
    end

    def reachable_objects_from_root
      roots = {}
      consts = []
      Object.constants.each do |c|
        begin
          v = Object.const_get(c)
          consts << v unless __is_immediate(v)
        rescue Exception
        end
      end
      roots["vm"] = consts
      roots
    end

    def __json_escape(str)
      return '"' + str + '"' if str.ascii_only? && !str.match?(/["\\\x00-\x1f]/)

      out = +'"'
      str.each_char do |ch|
        case ch
        when '"'  then out << '\"'
        when '\\' then out << '\\\\'
        when "\n" then out << '\n'
        when "\r" then out << '\r'
        when "\t" then out << '\t'
        else
          ord = ch.ord
          if ord < 0x20
            out << format('\u%04x', ord)
          else
            out << ch
          end
        end
      end
      out << '"'
    end

    def __dump_object_json(obj)
      tsym = __type_sym(obj)
      type_name = tsym.to_s.delete_prefix("T_")
      parts = []
      addr = __obj_address(obj)
      parts << "\"address\":#{__json_escape(addr)}"
      parts << "\"type\":#{__json_escape(type_name)}"
      cls = internal_class_of(obj)
      parts << "\"class\":#{__json_escape(__obj_address(cls))}" if cls
      case tsym
      when :T_STRING
        parts << "\"bytesize\":#{obj.bytesize}"
        if obj.bytesize <= 256 && obj.encoding == Encoding::UTF_8
          parts << "\"value\":#{__json_escape(obj)}"
        end
        parts << "\"encoding\":#{__json_escape(obj.encoding.name)}"
      when :T_ARRAY
        parts << "\"length\":#{obj.length}"
      when :T_HASH
        parts << "\"size\":#{obj.size}"
      when :T_CLASS, :T_MODULE
        n = obj.name
        parts << "\"name\":#{__json_escape(n)}" if n
      end
      parts << "\"memsize\":#{memsize_of(obj)}"
      if (info = __oruby_allocation_info(obj))
        parts << "\"file\":#{__json_escape(info[0])}" if info[0]
        parts << "\"line\":#{info[1]}" if info[1]
        parts << "\"method\":#{__json_escape(info[3].to_s)}" if info[3]
        parts << "\"generation\":#{info[4]}" if info[4]
      end
      "{#{parts.join(', ')}}\n"
    end

    def dump(obj, output: :string)
      line = __dump_object_json(obj)
      case output
      when :string
        line
      when :stdout
        $stdout.write(line)
        nil
      when :file, nil
        require "tempfile"
        owned = Tempfile.create(["orubyheap", ".json"])
        owned.write(line)
        owned.flush
        owned
      when IO
        output.write(line)
        output
      else
        raise ArgumentError, "wrong output option: #{output.inspect}"
      end
    rescue Exception
      if owned
        owned.close rescue nil
        File.unlink(owned.path) rescue nil
      end
      raise
    end

    def dump_all(output: :file, full: false, since: nil, shapes: true)
      buf = nil
      io =
        case output
        when :string
          buf = +""
          nil
        when :stdout
          $stdout
        when :file, nil
          require "tempfile"
          owned = Tempfile.create(["orubyheap", ".json"])
        when IO
          output
        else
          raise ArgumentError, "wrong output option: #{output.inspect}"
        end

      each_object do |o|
        if since
          gen = allocation_generation(o)
          next unless gen && gen >= since
        end
        line = __dump_object_json(o)
        if buf
          buf << line
        else
          io.write(line)
        end
      end

      if buf
        buf
      elsif output == :stdout
        nil
      else
        io.flush if io.respond_to?(:flush)
        io
      end
    rescue Exception
      if owned
        owned.close rescue nil
        File.unlink(owned.path) rescue nil
      end
      raise
    end

    def dump_shapes(output: :file, since: 0)
      case output
      when :string then ""
      when :stdout then nil
      when :file
        require "tempfile"
        Tempfile.new(["orubyshapes", ".json"])
      else
        output
      end
    end

    def trace_object_allocations_start = __oruby_alloc_trace_start

    alias trace_object_allocations_debug_start trace_object_allocations_start

    def trace_object_allocations_stop = __oruby_alloc_trace_stop

    def trace_object_allocations_clear = __oruby_alloc_trace_clear

    def trace_object_allocations
      trace_object_allocations_start
      begin
        yield
      ensure
        trace_object_allocations_stop
      end
    end

    def allocation_sourcefile(obj)
      info = __oruby_allocation_info(obj)
      info ? info[0] : nil
    end

    def allocation_sourceline(obj)
      info = __oruby_allocation_info(obj)
      info ? info[1] : nil
    end

    def allocation_class_path(obj)
      info = __oruby_allocation_info(obj)
      info ? info[2] : nil
    end

    def allocation_method_id(obj)
      info = __oruby_allocation_info(obj)
      info ? info[3] : nil
    end

    def allocation_generation(obj)
      info = __oruby_allocation_info(obj)
      info ? info[4] : nil
    end
  end
end
