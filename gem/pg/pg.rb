module PG
  class Error
    attr_reader :connection, :result

    def initialize(msg = nil, connection: nil, result: nil)
      @connection = connection
      @result = result
      super(msg)
    end
  end

  def self.require_bigdecimal_without_warning
    require "bigdecimal"
  end

  def self.make_shareable(obj)
    obj.freeze
  end

  def self.init_openssl(_do_ssl = true, _do_crypto = true)
    nil
  end

  def self.init_ssl(_do_ssl = true)
    nil
  end

  def self.threadsafe?
    true
  end
  class << self
    alias isthreadsafe threadsafe?
    alias is_threadsafe? threadsafe?
  end

  def self.version_string(include_buildnum = false)
    v = "PG #{VERSION}"
    v = "#{v} (oruby pgx)" if include_buildnum
    v
  end

  def self.connect(*args, **kw, &block)
    Connection.open(*args, **kw, &block)
  end

  class Connection
    @async_api = true

    class << self
      attr_accessor :async_api

      def open(*args, **kw)
        conn = kw.empty? ? new(*args) : new(*args, **kw)
        return conn unless block_given?
        begin
          yield conn
        ensure
          conn.close unless conn.finished?
        end
      end

      alias connect open
      alias isthreadsafe threadsafe? if respond_to?(:threadsafe?)

      def ping(*args, **kw)
        conn = kw.empty? ? new(*args) : new(*args, **kw)
        conn.close
        PQPING_OK
      rescue PG::ConnectionBad
        PQPING_NO_RESPONSE
      end

      def quote_ident(str)
        s = str.to_s
        '"' + s.gsub('"', '""') + '"'
      end

      def escape_string(str)
        str.to_s.gsub("'", "''")
      end
      alias escape escape_string

      def conndefaults
        [
          { keyword: "host", envvar: "PGHOST", compiled: nil, val: ENV["PGHOST"], label: "Database-host", dispchar: "", dispsize: 40 },
          { keyword: "port", envvar: "PGPORT", compiled: "5432", val: ENV["PGPORT"] || "5432", label: "Database-port", dispchar: "", dispsize: 6 },
          { keyword: "dbname", envvar: "PGDATABASE", compiled: nil, val: ENV["PGDATABASE"], label: "Database-name", dispchar: "", dispsize: 20 },
          { keyword: "user", envvar: "PGUSER", compiled: nil, val: ENV["PGUSER"] || ENV["USER"], label: "Database-user", dispchar: "", dispsize: 20 },
          { keyword: "password", envvar: "PGPASSWORD", compiled: nil, val: ENV["PGPASSWORD"], label: "Database-password", dispchar: "*", dispsize: 20 },
        ]
      end

      def conndefaults_hash
        h = {}
        conndefaults.each { |d| h[d[:keyword].to_sym] = d[:val] }
        h
      end

      def parse_connect_args(*args)
        return "" if args.empty?
        if args.length == 1 && args[0].is_a?(String)
          args[0]
        elsif args.length == 1 && args[0].is_a?(Hash)
          args[0].reject { |_, v| v.nil? }.map { |k, v| "#{k}='#{v.to_s.gsub("'", "\\'")}'" }.join(" ")
        else
          keys = [:host, :port, :options, :tty, :dbname, :user, :password]
          pairs = []
          args.each_with_index do |v, i|
            next if v.nil? || keys[i].nil?
            pairs << "#{keys[i]}='#{v.to_s.gsub("'", "\\'")}'"
          end
          pairs.join(" ")
        end
      end
    end

    alias sync_exec exec
    alias async_exec exec
    alias sync_query query
    alias async_query query
    alias sync_exec_params exec_params
    alias async_exec_params exec_params
    alias sync_exec_prepared exec_prepared
    alias async_exec_prepared exec_prepared
    alias sync_prepare prepare
    alias async_prepare prepare
    alias sync_describe_prepared describe_prepared
    alias async_describe_prepared describe_prepared
    alias sync_set_client_encoding set_client_encoding if method_defined?(:set_client_encoding)

    def options
      ""
    end

    def tty
      ""
    end

    def reset_start
      reset
      true
    end

    def reset_poll
      finished? ? PGRES_POLLING_FAILED : PGRES_POLLING_OK
    end

    def flush
      true
    end

    def set_single_row_mode
      nil
    end

    def set_chunked_rows_mode(_size = 1)
      nil
    end

    SAFE_TYPE_NAME_PART_RE = /\A[A-Za-z_][A-Za-z0-9_$]*\z/
    private_constant :SAFE_TYPE_NAME_PART_RE

    def quote_type_name(name)
      name = name.to_s.dup
      array_suffix = name.slice!(/(?:\[\])+\z/) || ""
      name = name.split(".", -1).map do |part|
        SAFE_TYPE_NAME_PART_RE.match?(part) ? part : quote_ident(part)
      end.join(".")
      name + array_suffix
    end
    private :quote_type_name

    def embed_params(sql, params, type_map: type_map_for_queries)
      return sql if params.empty?

      oid_to_typecast = proc do |oid, tname, errtext|
        if oid && oid > 0
          if tname.to_s.empty?
            raise ArgumentError, "Database type name of OID #{oid.inspect} missing#{errtext}"
          else
            "::#{quote_type_name(tname)}"
          end
        end
      end

      encoders = type_map.query_param_encoders(params)
      encoded = encoders.map.with_index do |enc, i|
        value = params[i]
        case value
        when NilClass
          "NULL"
        when PG::BasicTypeMapForQueries::BinaryData
          "'#{escape_bytea(value)}'"
        else
          if enc
            raise ArgumentError, "binary encoded parameter from #{enc.inspect} cannot be inserted into SQL text" if enc.format != 0
            "'#{escape(enc.encode(value))}'#{oid_to_typecast[enc.oid, enc.name, " - please set name of encoder #{enc.inspect}"]}"
          elsif Hash === value
            typename = value[:format] == 1 && value[:type] == 17 ?
              "::bytea" : oid_to_typecast[value[:type], value[:typename], " - please set parameter key :typename"]
            case value[:value]
            when NilClass
              "NULL#{typename}"
            else
              if value[:format] == 1
                raise ArgumentError, "binary encoded parameter with OID #{value[:type].inspect} cannot be inserted into SQL text" if value[:type] && value[:type] != 17
                "'#{escape_bytea(value[:value].to_s)}'#{typename}"
              else
                "'#{escape(value[:value].to_s)}'#{typename}"
              end
            end
          else
            "'#{escape(value.to_s)}'"
          end
        end
      end

      out = +""
      i = 0
      n = sql.length
      while i < n
        ch = sql[i]
        if ch == "'"
          j = i + 1
          while j < n
            if sql[j] == "'"
              if sql[j + 1] == "'"
                j += 2
              else
                j += 1
                break
              end
            else
              j += 1
            end
          end
          out << sql[i...j]
          i = j
        elsif ch == '"'
          j = i + 1
          while j < n
            if sql[j] == '"'
              if sql[j + 1] == '"'
                j += 2
              else
                j += 1
                break
              end
            else
              j += 1
            end
          end
          out << sql[i...j]
          i = j
        elsif ch == "-" && sql[i + 1] == "-"
          j = sql.index("\n", i) || n
          out << sql[i...j]
          i = j
        elsif ch == "/" && sql[i + 1] == "*"
          j = sql.index("*/", i + 2)
          j = j ? j + 2 : n
          out << sql[i...j]
          i = j
        elsif ch == "$"
          if sql[i + 1] == "$"
            j = sql.index("$$", i + 2)
            j = j ? j + 2 : n
            out << sql[i...j]
            i = j
          elsif (m = /\A\$([A-Za-z_][A-Za-z_0-9]*)\$/.match(sql, i))
            tag = m[0]
            j = sql.index(tag, i + tag.length)
            j = j ? j + tag.length : n
            out << sql[i...j]
            i = j
          elsif (m = /\A\$([1-9]\d*)/.match(sql, i))
            idx = m[1].to_i - 1
            out << (encoded[idx] || "")
            i += m[0].length
          else
            out << ch
            i += 1
          end
        else
          out << ch
          i += 1
        end
      end
      out
    end

    BINARY_SIGNATURE = "PGCOPY\n\377\r\n\0".b
    private_constant :BINARY_SIGNATURE

    def copy_data(sql, coder = nil)
      raise PG::NotInBlockingMode.new("copy_data can not be used in nonblocking mode", connection: self) if nonblocking?
      res = exec(sql)
      case res.result_status
      when PGRES_COPY_IN
        old_coder = nil
        begin
          if coder && res.binary_tuples == 1
            put_copy_data(BINARY_SIGNATURE + ("\x00".b * 8))
          end
          if coder
            old_coder = self.encoder_for_put_copy_data
            self.encoder_for_put_copy_data = coder
          end
          yield res
        rescue Exception => err
          errmsg = "#{err.class.name} while copy data: #{err.message}"
          begin
            put_copy_end(errmsg)
          rescue PG::Error
          end
          discard_results
          raise err
        else
          begin
            self.encoder_for_put_copy_data = old_coder if coder
            if coder && res.binary_tuples == 1
              put_copy_data("\xFF\xFF".b)
            end
            put_copy_end
          rescue PG::Error => err
            raise PG::LostCopyState.new("#{err} (probably by executing another SQL query while running a COPY command)", connection: self)
          end
          get_last_result
        ensure
          self.encoder_for_put_copy_data = old_coder if coder
        end
      when PGRES_COPY_OUT
        old_coder = nil
        begin
          if coder
            old_coder = self.decoder_for_get_copy_data
            self.decoder_for_get_copy_data = coder
          end
          yield res
        rescue Exception
          cancel
          discard_results
          raise
        else
          if coder && res.binary_tuples == 1
            if get_copy_data
              discard_results
              raise PG::NotAllCopyDataRetrieved.new("Not all binary COPY data retrieved", connection: self)
            end
          end
          res2 = get_last_result
          if !res2
            discard_results
            raise PG::LostCopyState.new("Lost COPY state (probably by executing another SQL query while running a COPY command)", connection: self)
          elsif res2.result_status != PGRES_COMMAND_OK
            discard_results
            raise PG::NotAllCopyDataRetrieved.new("Not all COPY data retrieved", connection: self)
          end
          res2
        ensure
          self.decoder_for_get_copy_data = old_coder if coder
        end
      else
        raise PG::InvalidResultStatus.new("unexpected result status #{res.result_status} for COPY", connection: self, result: res)
      end
    end

    def set_error_verbosity(v)
      old = @error_verbosity || PQERRORS_DEFAULT
      @error_verbosity = v
      old
    end

    def set_error_context_visibility(v)
      old = @error_context_visibility || PQSHOW_CONTEXT_ERRORS
      @error_context_visibility = v
      old
    end

    def trace(io)
      @trace_io = io
      nil
    end

    def untrace
      @trace_io = nil
      nil
    end

    def set_notice_processor(&block)
      old = @notice_processor
      @notice_processor = block
      old
    end

    def set_notice_receiver(&block)
      old = @notice_receiver
      @notice_receiver = block
      old
    end

    def set_client_encoding(enc)
      @client_encoding = enc.to_s
      exec("SET client_encoding TO '#{escape_string(@client_encoding)}'")
      0
    end
    alias client_encoding= set_client_encoding

    def client_encoding
      @client_encoding || parameter_status("client_encoding") || "UTF8"
    end

    def set_default_encoding
      @internal_encoding = Encoding::UTF_8
      @external_encoding = Encoding::UTF_8
    end

    def internal_encoding
      @internal_encoding
    end

    def internal_encoding=(enc)
      @internal_encoding = enc
    end

    def external_encoding
      @external_encoding || Encoding::UTF_8
    end

    def setnonblocking(flag)
      @nonblocking = !!flag
      nil
    end

    def isnonblocking
      !!@nonblocking
    end
    alias nonblocking? isnonblocking

    attr_accessor :encoder_for_put_copy_data, :decoder_for_get_copy_data

    def transaction
      exec("BEGIN")
      begin
        res = yield self
      rescue Exception
        begin
          exec("ROLLBACK")
        rescue Exception
        end
        raise
      end
      exec("COMMIT")
      res
    end

    def conninfo
      [
        { keyword: "host", val: host },
        { keyword: "port", val: port.to_s },
        { keyword: "dbname", val: db },
        { keyword: "user", val: user },
        { keyword: "password", val: pass },
      ]
    end

    def conninfo_hash
      {
        host: host,
        port: port.to_s,
        dbname: db,
        user: user,
      }
    end
  end

  class Tuple
    include Enumerable

    UNSET = Object.new.freeze
    private_constant :UNSET

    class << self
      undef_method :new
    end

    def self.__build(result, index, keys)
      t = allocate
      t.__init(result, index, keys)
      t
    end

    def __init(result, index, keys)
      @result = result
      @index = index
      @keys = keys
      @values = Array.new(keys.length, UNSET)
    end

    def __val(idx)
      v = @values[idx]
      if v.equal?(UNSET)
        v = @result.getvalue(@index, idx)
        @values[idx] = v unless @values.frozen?
      end
      v
    end
    private :__val

    def freeze
      if @result
        @values.length.times { |i| __val(i) }
        @result = nil
      end
      @keys.freeze
      @values.freeze
      super
    end

    def [](key)
      if key.is_a?(Integer)
        idx = key < 0 ? @keys.length + key : key
        return nil if idx < 0 || idx >= @keys.length
        __val(idx)
      elsif key.is_a?(String) || key.is_a?(Symbol)
        idx = @keys.rindex(key)
        idx ? __val(idx) : nil
      else
        nil
      end
    end

    def fetch(key, *default, &block)
      if key.is_a?(Integer)
        idx = key < 0 ? @keys.length + key : key
        if idx >= 0 && idx < @keys.length
          return __val(idx)
        end
        return yield(key) if block
        return default[0] unless default.empty?
        raise IndexError, "index #{key} outside of tuple bounds: #{-@keys.length}...#{@keys.length}"
      elsif key.is_a?(String) || key.is_a?(Symbol)
        idx = @keys.rindex(key)
        return __val(idx) if idx
      end
      return yield(key) if block
      return default[0] unless default.empty?
      raise KeyError, "key not found: #{key.inspect}"
    end

    def each
      return to_enum(:each) unless block_given?
      @keys.each_with_index { |k, i| yield k, __val(i) }
      self
    end

    def each_key(&block)
      return to_enum(:each_key) unless block_given?
      @keys.each(&block)
      self
    end

    def each_value
      return to_enum(:each_value) unless block_given?
      @keys.length.times { |i| yield __val(i) }
      self
    end

    def keys
      @keys.dup
    end
    alias field_names keys

    def values
      Array.new(@keys.length) { |i| __val(i) }
    end
    alias field_values values

    def values_at(*args)
      args.map { |k| self[k] }
    end

    def size
      @keys.length
    end
    alias length size

    def empty?
      @keys.empty?
    end

    def has_key?(key)
      @keys.include?(key)
    end
    alias key? has_key?
    alias include? has_key?

    def index(key)
      @keys.index(key)
    end

    def to_h
      h = {}
      @keys.each_with_index { |k, i| h[k] = __val(i) }
      h
    end
    alias to_hash to_h

    def ==(other)
      other.is_a?(Tuple) && @keys == other.keys && values == other.values
    end
    alias eql? ==

    def hash
      [@keys, values].hash
    end

    def inspect
      pairs = @keys.each_with_index.map { |k, i| "#{k.inspect}=>#{__val(i).inspect}" }
      "#<PG::Tuple #{pairs.join(', ')}>"
    end

    def marshal_dump
      [@keys, values]
    end

    def marshal_load(ary)
      @result = nil
      @index = nil
      @keys, @values = ary
    end
  end

  class Result
    def self.res_status(status)
      case status
      when PGRES_EMPTY_QUERY     then "PGRES_EMPTY_QUERY"
      when PGRES_COMMAND_OK      then "PGRES_COMMAND_OK"
      when PGRES_TUPLES_OK       then "PGRES_TUPLES_OK"
      when PGRES_COPY_OUT        then "PGRES_COPY_OUT"
      when PGRES_COPY_IN         then "PGRES_COPY_IN"
      when PGRES_BAD_RESPONSE    then "PGRES_BAD_RESPONSE"
      when PGRES_NONFATAL_ERROR  then "PGRES_NONFATAL_ERROR"
      when PGRES_FATAL_ERROR     then "PGRES_FATAL_ERROR"
      when PGRES_COPY_BOTH       then "PGRES_COPY_BOTH"
      when PGRES_SINGLE_TUPLE    then "PGRES_SINGLE_TUPLE"
      when PGRES_PIPELINE_SYNC   then "PGRES_PIPELINE_SYNC"
      when PGRES_PIPELINE_ABORTED then "PGRES_PIPELINE_ABORTED"
      else "unknown result status"
      end
    end

    def res_status(status = result_status)
      self.class.res_status(status)
    end

    def field_names_as(type)
      self.field_name_type = type
      self
    end

    def tuple(index)
      idx = index < 0 ? ntuples + index : index
      raise IndexError, "Index #{index} is out of range" if idx < 0 || idx >= ntuples
      ks = field_name_type == :symbol ? fields.map(&:to_sym) : fields
      Tuple.__build(self, idx, ks)
    end

    def each
      return enum_for(:each) { ntuples } unless block_given?
      ntuples.times { |i| yield self[i] }
      self
    end
    alias stream_each each

    def each_row
      return enum_for(:each_row) { ntuples } unless block_given?
      ntuples.times { |i| yield tuple_values(i) }
      self
    end
    alias stream_each_row each_row

    def each_tuple
      return enum_for(:each_tuple) { ntuples } unless block_given?
      ntuples.times { |i| yield tuple(i) }
      self
    end
    alias stream_each_tuple each_tuple

    def field_values(name)
      idx = fnumber(name)
      raise IndexError, "no such field #{name.inspect}" if idx < 0
      column_values(idx)
    end

    def autoclear?
      false
    end

    def result_verbose_error_message(verbosity = PQERRORS_DEFAULT, _show_context = PQSHOW_CONTEXT_ERRORS)
      sev = error_field(PG_DIAG_SEVERITY) || "ERROR"
      sqlstate = error_field(PG_DIAG_SQLSTATE)
      primary = error_field(PG_DIAG_MESSAGE_PRIMARY) || error_message
      case verbosity
      when PQERRORS_TERSE
        "#{sev}:  #{primary}\n"
      when PQERRORS_SQLSTATE
        sqlstate ? "#{sev}:  #{sqlstate}: #{primary}\n" : "#{sev}:  #{primary}\n"
      when PQERRORS_VERBOSE
        loc = [error_field(PG_DIAG_SOURCE_FUNCTION), error_field(PG_DIAG_SOURCE_FILE), error_field(PG_DIAG_SOURCE_LINE)].compact.join(", ")
        "#{sev}:  #{sqlstate}: #{primary}\nLOCATION:  #{loc}\n"
      else
        "#{sev}:  #{primary}\n"
      end
    end
    alias verbose_error_message result_verbose_error_message
  end
end
