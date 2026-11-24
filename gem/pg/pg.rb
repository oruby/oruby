begin
  require "date"
rescue LoadError
end

module PG
  class Error
    attr_reader :connection, :result

    def initialize(msg = nil, connection: nil, result: nil)
      @connection = connection
      @result = result
      super(msg)
    end
  end

  class RollbackTransaction < StandardError
  end

  DuplicatePstatement = DuplicatePreparedStatement unless const_defined?(:DuplicatePstatement)

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
    PROGRAM_NAME = PG.make_shareable($0.to_s.dup)

    KNOWN_CONN_OPTIONS = %w[
      host hostaddr port dbname user password passfile require_auth
      channel_binding connect_timeout client_encoding options
      application_name fallback_application_name keepalives keepalives_idle
      keepalives_interval keepalives_count tcp_user_timeout
      replication gssencmode sslmode requiressl sslnegotiation
      sslcompression sslcert sslkey sslpassword sslcertmode sslrootcert
      sslcrl sslcrldir sslsni requirepeer ssl_min_protocol_version
      ssl_max_protocol_version krbsrvname gsslib gssdelegation
      service target_session_attrs load_balance_hosts tty
      min_protocol_version max_protocol_version
    ].freeze
    private_constant :KNOWN_CONN_OPTIONS

    CONN_DEFAULTS_SPEC = [
      ["user", "PGUSER", nil, "Database-user", "", 20],
      ["password", "PGPASSWORD", nil, "Database-password", "*", 20],
      ["dbname", "PGDATABASE", nil, "Database-name", "", 20],
      ["host", "PGHOST", nil, "Database-host", "", 40],
      ["hostaddr", "PGHOSTADDR", nil, "Database-host-IPv4-address", "", 45],
      ["port", "PGPORT", "5432", "Database-port", "", 6],
      ["options", "PGOPTIONS", nil, "Backend-Debug-Options", "", 40],
      ["tty", "PGTTY", nil, "Backend-Debug-TTY", "", 40],
      ["fallback_application_name", nil, nil, "Fallback-Application-Name", "", 64],
      ["application_name", "PGAPPNAME", nil, "Application-Name", "", 64],
      ["sslmode", "PGSSLMODE", "prefer", "SSL-Mode", "", 12],
      ["connect_timeout", "PGCONNECT_TIMEOUT", nil, "Connect-timeout", "", 10],
      ["service", "PGSERVICE", nil, "Database-service", "", 20],
      ["client_encoding", "PGCLIENTENCODING", nil, "Client-Encoding", "", 20],
      ["target_session_attrs", "PGTARGETSESSIONATTRS", nil, "Target-Session-Attrs", "", 20],
      ["keepalives", nil, nil, "TCP-Keepalives", "", 1],
      ["keepalives_idle", nil, nil, "TCP-Keepalives-Idle", "", 10],
      ["keepalives_interval", nil, nil, "TCP-Keepalives-Interval", "", 10],
      ["keepalives_count", nil, nil, "TCP-Keepalives-Count", "", 10],
      ["sslcert", "PGSSLCERT", nil, "SSL-Client-Cert", "", 64],
      ["sslkey", "PGSSLKEY", nil, "SSL-Client-Key", "", 64],
      ["sslrootcert", "PGSSLROOTCERT", nil, "SSL-Root-Cert", "", 64],
    ].freeze
    private_constant :CONN_DEFAULTS_SPEC

    class << self
      attr_accessor :async_api

      def new(*args, **kw, &block)
        conn = allocate
        if kw.empty?
          conn.send(:initialize, *args)
        else
          conn.send(:initialize, *args, **kw)
        end
        return conn unless block
        begin
          yield conn
        ensure
          conn.close unless conn.finished?
        end
      end

      alias open new
      alias connect new
      alias sync_connect new
      alias async_connect new
      alias setdb new
      alias setdblogin new
      alias isthreadsafe threadsafe? if respond_to?(:threadsafe?)

      def ping(*args, **kw)
        begin
          parsed_str = kw.empty? ? parse_connect_args(*args) : parse_connect_args(*args, **kw)
          parsed = conninfo_parse(parsed_str)
          parsed.each do |entry|
            if !entry[:val].nil? && !KNOWN_CONN_OPTIONS.include?(entry[:keyword])
              return PQPING_NO_ATTEMPT
            end
          end
        rescue ArgumentError, PG::Error
          return PQPING_NO_ATTEMPT
        end
        conn = kw.empty? ? new(*args) : new(*args, **kw)
        conn.close
        PQPING_OK
      rescue PG::ConnectionBad => e
        msg = e.message.to_s
        if msg.include?("cannot parse") || msg.include?("invalid port") || msg.include?("unrecognized configuration parameter")
          PQPING_NO_ATTEMPT
        else
          PQPING_NO_RESPONSE
        end
      end

      def conndefaults
        CONN_DEFAULTS_SPEC.map do |kw, envvar, compiled, label, dispchar, dispsize|
          val = envvar ? ENV[envvar] : nil
          val = compiled if val.nil?
          val = ENV["USER"] if kw == "user" && val.nil?
          { keyword: kw, envvar: envvar, compiled: compiled, val: val, label: label, dispchar: dispchar, dispsize: dispsize }
        end
      end

      def conndefaults_hash
        h = {}
        conndefaults.each { |d| h[d[:keyword].to_sym] = d[:val] }
        h
      end

      def quote_connstr(v)
        bs = "\\"
        bs2 = "\\\\"
        sq = "'"
        sqe = "\\'"
        "'" + v.to_s.gsub(bs) { bs2 }.gsub(sq) { sqe } + "'"
      end

      def connect_hash_to_string(hash)
        hash.reject { |_, v| v.nil? }.map { |k, v| "#{k}=#{quote_connstr(v)}" }.join(" ")
      end

      def __fallback_app_name
        name = PROGRAM_NAME.to_s
        return name if name.length <= 64
        name[0, 30] + "..." + name[-31, 31]
      end
      private :__fallback_app_name

      def parse_connect_args(*args)
        hash = {}
        if !args.empty? && args.last.is_a?(Hash)
          extra_hash = args.pop
        else
          extra_hash = nil
        end

        if args.length > 7
          raise ArgumentError, "extra positional parameter #{args.length}: #{args[7].inspect}"
        end

        if args.length == 1
          a0 = args[0]
          is_uri = (defined?(URI::Generic) && a0.is_a?(URI::Generic)) ||
                   (a0.respond_to?(:scheme) && a0.respond_to?(:to_s) && !a0.is_a?(String))
          s = a0.to_s
          if is_uri || s.start_with?("postgres://", "postgresql://") || s.include?("=")
            conninfo_parse(s).each do |entry|
              hash[entry[:keyword]] = entry[:val] unless entry[:val].nil?
            end
          elsif !a0.nil?
            hash["host"] = s
          end
        elsif args.length > 1
          keys = %w[host port options tty dbname user password]
          args.each_with_index do |v, i|
            next if v.nil?
            hash[keys[i]] = v.to_s
          end
        end

        ordered = {}
        CONN_DEFAULTS_SPEC.each do |kw, *|
          ordered[kw] = hash[kw] if hash.key?(kw)
        end
        hash.each do |k, v|
          ordered[k] = v unless ordered.key?(k)
        end

        if extra_hash
          extra_hash.each do |k, v|
            ks = k.to_s
            if v.nil?
              ordered.delete(ks)
            else
              ordered[ks] = v.to_s
            end
          end
        end

        if !ordered.key?("application_name") && !ordered.key?("fallback_application_name")
          ordered["fallback_application_name"] = __fallback_app_name
        end

        connect_hash_to_string(ordered)
      end

      def __pct_decode(str)
        str.gsub(/%([0-9A-Fa-f]{2})/) { $1.to_i(16).chr }
      end
      private :__pct_decode

      def conninfo_parse(str)
        s = str.to_s.strip
        defaults = CONN_DEFAULTS_SPEC.map do |kw, envvar, compiled, label, dispchar, dispsize|
          { keyword: kw, envvar: envvar, compiled: compiled, val: nil, label: label, dispchar: dispchar, dispsize: dispsize }
        end
        by_key = {}
        defaults.each { |d| by_key[d[:keyword]] = d }
        set_kv = lambda do |k, v|
          if by_key[k]
            by_key[k][:val] = v
          else
            entry = { keyword: k, envvar: nil, compiled: nil, val: v, label: k, dispchar: "", dispsize: 20 }
            defaults << entry
            by_key[k] = entry
          end
        end
        if s.start_with?("postgres://", "postgresql://")
          rest = s.sub(/\Apostgres(?:ql)?:\/\//, "")
          query_str = nil
          if (qidx = rest.index("?"))
            query_str = rest[(qidx + 1)..]
            rest = rest[0...qidx]
          end
          path_str = nil
          if (pidx = rest.index("/"))
            path_str = rest[(pidx + 1)..]
            rest = rest[0...pidx]
          end
          authority = rest
          if (at_idx = authority.rindex("@"))
            userinfo = authority[0...at_idx]
            hostport = authority[(at_idx + 1)..]
            if (cidx = userinfo.index(":"))
              set_kv.call("user", __pct_decode(userinfo[0...cidx]))
              set_kv.call("password", __pct_decode(userinfo[(cidx + 1)..]))
            else
              set_kv.call("user", __pct_decode(userinfo)) unless userinfo.empty?
            end
          else
            hostport = authority
          end
          unless hostport.empty?
            hosts = []
            ports = []
            has_port = false
            hostport.split(",", -1).each do |hp|
              if hp.start_with?("[")
                if (rb_idx = hp.index("]"))
                  h = hp[1...rb_idx]
                  after = hp[(rb_idx + 1)..]
                  if after.start_with?(":")
                    hosts << __pct_decode(h)
                    ports << after[1..]
                    has_port = true
                  else
                    hosts << __pct_decode(h)
                    ports << ""
                  end
                else
                  hosts << __pct_decode(hp)
                  ports << ""
                end
              elsif (cidx = hp.rindex(":"))
                hosts << __pct_decode(hp[0...cidx])
                ports << hp[(cidx + 1)..]
                has_port = true
              else
                hosts << __pct_decode(hp)
                ports << ""
              end
            end
            hstr = hosts.join(",")
            set_kv.call("host", hstr) unless hstr.empty?
            set_kv.call("port", ports.join(",")) if has_port
          end
          if path_str && !path_str.empty?
            set_kv.call("dbname", __pct_decode(path_str))
          end
          if query_str && !query_str.empty?
            query_str.split("&").each do |pair|
              k, v = pair.split("=", 2)
              next unless k && !k.empty?
              set_kv.call(__pct_decode(k), v ? __pct_decode(v) : "")
            end
          end
        else
          i = 0
          n = s.length
          while i < n
            i += 1 while i < n && s[i] =~ /\s/
            break if i >= n
            eq = s.index("=", i)
            raise PG::Error, "missing \"=\" after \"#{s[i..]}\" in connection info string" unless eq
            k = s[i...eq].strip
            raise PG::Error, "missing \"=\" after \"#{s[i..]}\" in connection info string" if k.empty?
            i = eq + 1
            i += 1 while i < n && s[i] =~ /\s/
            if i < n && s[i] == "'"
              i += 1
              v = +""
              while i < n && s[i] != "'"
                if s[i] == "\\" && i + 1 < n
                  v << s[i + 1]
                  i += 2
                else
                  v << s[i]
                  i += 1
                end
              end
              raise PG::Error, "unterminated quoted string in connection info string" unless i < n && s[i] == "'"
              i += 1
            else
              v = +""
              while i < n && s[i] !~ /\s/
                if s[i] == "\\" && i + 1 < n
                  v << s[i + 1]
                  i += 2
                else
                  v << s[i]
                  i += 1
                end
              end
            end
            set_kv.call(k, v)
          end
        end
        defaults
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
    alias sync_describe_portal describe_portal if method_defined?(:describe_portal)
    alias async_describe_portal describe_portal if method_defined?(:describe_portal)
    alias sync_set_client_encoding set_client_encoding if method_defined?(:set_client_encoding)

    CONN_STATUS_NAMES = {
      CONNECTION_OK => "CONNECTION_OK",
      CONNECTION_BAD => "CONNECTION_BAD",
      CONNECTION_STARTED => "CONNECTION_STARTED",
      CONNECTION_MADE => "CONNECTION_MADE",
      CONNECTION_AWAITING_RESPONSE => "CONNECTION_AWAITING_RESPONSE",
      CONNECTION_AUTH_OK => "CONNECTION_AUTH_OK",
      CONNECTION_SETENV => "CONNECTION_SETENV",
      CONNECTION_SSL_STARTUP => "CONNECTION_SSL_STARTUP",
      CONNECTION_NEEDED => "CONNECTION_NEEDED",
      CONNECTION_CHECK_WRITABLE => "CONNECTION_CHECK_WRITABLE",
      CONNECTION_CONSUME => "CONNECTION_CONSUME",
    }.freeze
    private_constant :CONN_STATUS_NAMES

    TX_STATUS_NAMES = {
      PQTRANS_IDLE => "PQTRANS_IDLE",
      PQTRANS_ACTIVE => "PQTRANS_ACTIVE",
      PQTRANS_INTRANS => "PQTRANS_INTRANS",
      PQTRANS_INERROR => "PQTRANS_INERROR",
      PQTRANS_UNKNOWN => "PQTRANS_UNKNOWN",
    }.freeze
    private_constant :TX_STATUS_NAMES

    PIPELINE_STATUS_NAMES = {
      PQ_PIPELINE_OFF => "PQ_PIPELINE_OFF",
      PQ_PIPELINE_ON => "PQ_PIPELINE_ON",
      PQ_PIPELINE_ABORTED => "PQ_PIPELINE_ABORTED",
    }.freeze
    private_constant :PIPELINE_STATUS_NAMES

    def inspect
      id = "0x#{object_id.to_s(16)}"
      return "#<#{self.class}:#{id} finished>" if finished?
      parts = ["#<#{self.class}:#{id}"]
      parts << "host=#{host}"
      parts << "port=#{port}"
      parts << "user=#{user}"
      st = status
      parts << "status=#{CONN_STATUS_NAMES[st] || st}" if st != CONNECTION_OK
      ps = pipeline_status
      parts << "pipeline_status=#{PIPELINE_STATUS_NAMES[ps] || ps}" if ps != PQ_PIPELINE_OFF
      ts = transaction_status
      parts << "transaction_status=#{TX_STATUS_NAMES[ts] || ts}" if st == CONNECTION_OK && ts != PQTRANS_IDLE
      parts << "nonblocking=true" if nonblocking?
      if st == CONNECTION_OK
        ce = client_encoding
        parts << "client_encoding=#{ce}" if ce && ce.upcase != "UTF8"
      end
      tm_r = @__tm_res
      parts << "type_map_for_results=#<#{tm_r.class}:0x#{tm_r.object_id.to_s(16)}>" if tm_r && !tm_r.is_a?(PG::TypeMapAllStrings)
      tm_q = @__tm_qry
      parts << "type_map_for_queries=#<#{tm_q.class}:0x#{tm_q.object_id.to_s(16)}>" if tm_q && !tm_q.is_a?(PG::TypeMapAllStrings)
      parts << "encoder_for_put_copy_data=#<#{@encoder_for_put_copy_data.class}:0x#{@encoder_for_put_copy_data.object_id.to_s(16)}>" if @encoder_for_put_copy_data
      parts << "decoder_for_get_copy_data=#<#{@decoder_for_get_copy_data.class}:0x#{@decoder_for_get_copy_data.object_id.to_s(16)}>" if @decoder_for_get_copy_data
      parts.join(" ") + ">"
    end

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

    def reset_start2(*args, **kw)
      args.each do |a|
        if a.is_a?(String) && a.include?("\0")
          raise ArgumentError, "string contains null byte"
        end
      end
      str = kw.empty? ? self.class.parse_connect_args(*args) : self.class.parse_connect_args(*args, **kw)
      raise ArgumentError, "string contains null byte" if str.include?("\0")
      self.class.conninfo_parse(str)
      reset
      true
    end

    def reset_poll
      finished? ? PGRES_POLLING_FAILED : PGRES_POLLING_OK
    end

    def flush
      true
    end

    def set_chunked_rows_mode(_size = 1)
      set_single_row_mode
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
          elsif i + 1 < n && sql[i + 1] =~ /[A-Za-z_]/
            j = i + 2
            j += 1 while j < n && sql[j] =~ /[A-Za-z0-9_]/
            if j < n && sql[j] == "$"
              tag = sql[i..j]
              k = sql.index(tag, j + 1)
              k = k ? k + tag.length : n
              out << sql[i...k]
              i = k
            else
              out << ch
              i += 1
            end
          elsif i + 1 < n && sql[i + 1] >= "1" && sql[i + 1] <= "9"
            j = i + 2
            j += 1 while j < n && sql[j] >= "0" && sql[j] <= "9"
            idx = sql[(i + 1)...j].to_i - 1
            out << (encoded[idx] || "")
            i = j
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
        raise ArgumentError, "no COPY statement (got result status #{res.result_status})"
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

    PG_ENCODING_MAP = {
      "UTF8" => "UTF-8",
      "UNICODE" => "UTF-8",
      "UTF-8" => "UTF-8",
      "EUC_JP" => "EUC-JP",
      "EUC-JP" => "EUC-JP",
      "EUCJP" => "EUC-JP",
      "EUC_KR" => "EUC-KR",
      "EUC-KR" => "EUC-KR",
      "LATIN1" => "ISO-8859-1",
      "ISO8859-1" => "ISO-8859-1",
      "ISO-8859-1" => "ISO-8859-1",
      "LATIN2" => "ISO-8859-2",
      "ISO8859-2" => "ISO-8859-2",
      "ISO-8859-2" => "ISO-8859-2",
      "LATIN3" => "ISO-8859-3",
      "ISO8859-3" => "ISO-8859-3",
      "ISO-8859-3" => "ISO-8859-3",
      "LATIN4" => "ISO-8859-4",
      "ISO8859-4" => "ISO-8859-4",
      "ISO-8859-4" => "ISO-8859-4",
      "ISO_8859_5" => "ISO-8859-5",
      "ISO_8859_6" => "ISO-8859-6",
      "ISO_8859_7" => "ISO-8859-7",
      "ISO_8859_8" => "ISO-8859-8",
      "LATIN5" => "ISO-8859-9",
      "LATIN6" => "ISO-8859-10",
      "LATIN7" => "ISO-8859-13",
      "LATIN8" => "ISO-8859-14",
      "LATIN9" => "ISO-8859-15",
      "ISO8859-15" => "ISO-8859-15",
      "ISO-8859-15" => "ISO-8859-15",
      "LATIN10" => "ISO-8859-16",
      "KOI8R" => "KOI8-R",
      "KOI8-R" => "KOI8-R",
      "KOI8U" => "KOI8-U",
      "SQL_ASCII" => "ASCII-8BIT",
      "SJIS" => "Shift_JIS",
      "WIN1250" => "Windows-1250",
      "WIN1251" => "Windows-1251",
      "WIN1252" => "Windows-1252",
      "WIN1253" => "Windows-1253",
      "WIN1254" => "Windows-1254",
      "WIN1255" => "Windows-1255",
      "WIN1256" => "Windows-1256",
      "WIN1257" => "Windows-1257",
      "WIN1258" => "Windows-1258",
      "BIG5" => "Big5",
      "GBK" => "GBK",
      "GB18030" => "GB18030",
    }.freeze
    private_constant :PG_ENCODING_MAP

    RUBY_TO_PG_ENCODING = {
      "UTF-8" => "UTF8",
      "EUC-JP" => "EUC_JP",
      "EUC-KR" => "EUC_KR",
      "ISO-8859-1" => "LATIN1",
      "ISO8859-1" => "LATIN1",
      "ISO-8859-2" => "LATIN2",
      "ISO8859-2" => "LATIN2",
      "ISO-8859-3" => "LATIN3",
      "ISO-8859-4" => "LATIN4",
      "ISO-8859-5" => "ISO_8859_5",
      "ISO-8859-6" => "ISO_8859_6",
      "ISO-8859-7" => "ISO_8859_7",
      "ISO-8859-8" => "ISO_8859_8",
      "ISO-8859-9" => "LATIN5",
      "ISO-8859-10" => "LATIN6",
      "ISO-8859-13" => "LATIN7",
      "ISO-8859-14" => "LATIN8",
      "ISO-8859-15" => "LATIN9",
      "ISO8859-15" => "LATIN9",
      "ISO-8859-16" => "LATIN10",
      "KOI8-R" => "KOI8R",
      "KOI8-U" => "KOI8U",
      "ASCII-8BIT" => "SQL_ASCII",
      "BINARY" => "SQL_ASCII",
      "US-ASCII" => "SQL_ASCII",
      "SHIFT_JIS" => "SJIS",
      "WINDOWS-1250" => "WIN1250",
      "WINDOWS-1251" => "WIN1251",
      "WINDOWS-1252" => "WIN1252",
      "BIG5" => "BIG5",
      "GBK" => "GBK",
      "GB18030" => "GB18030",
    }.freeze
    private_constant :RUBY_TO_PG_ENCODING

    def __pg_to_ruby_encoding(pg_name)
      return Encoding::ASCII_8BIT unless pg_name
      rb_name = PG_ENCODING_MAP[pg_name.upcase]
      return Encoding::ASCII_8BIT unless rb_name
      Encoding.find(rb_name)
    rescue ArgumentError
      Encoding::ASCII_8BIT
    end
    private :__pg_to_ruby_encoding

    def set_client_encoding(enc)
      raise FrozenError, "can't modify frozen PG::Connection" if frozen?
      unless enc.is_a?(String)
        if enc.respond_to?(:to_str)
          enc = enc.to_str
        else
          raise TypeError, "no implicit conversion of #{enc.class} into String"
        end
      end
      pg_name = RUBY_TO_PG_ENCODING[enc.upcase] || enc
      exec("SET client_encoding TO '#{PG::Connection.escape_string(pg_name)}'")
      nil
    end
    alias client_encoding= set_client_encoding

    def client_encoding
      parameter_status("client_encoding") || "UTF8"
    end
    alias get_client_encoding client_encoding

    def set_default_encoding
      raise FrozenError, "can't modify frozen PG::Connection" if frozen?
      enc = Encoding.default_internal
      if enc
        pg_name = RUBY_TO_PG_ENCODING[enc.name.upcase] || enc.name
        if pg_name.upcase != client_encoding.upcase
          set_client_encoding(pg_name)
        end
      end
      nil
    end

    def internal_encoding
      __pg_to_ruby_encoding(client_encoding)
    end

    def internal_encoding=(enc)
      raise FrozenError, "can't modify frozen PG::Connection" if frozen?
      if enc.nil?
        set_client_encoding("SQL_ASCII")
        return nil
      end
      rb_enc = enc.is_a?(Encoding) ? enc : Encoding.find(enc.to_s)
      pg_name = RUBY_TO_PG_ENCODING[rb_enc.name.upcase] || rb_enc.name
      if pg_name.upcase != client_encoding.upcase
        set_client_encoding(pg_name)
      end
      rb_enc
    end

    def external_encoding
      __pg_to_ruby_encoding(parameter_status("server_encoding") || "UTF8")
    end

    def setnonblocking(flag)
      raise FrozenError, "can't modify frozen PG::Connection" if frozen?
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
      in_tx = true
      res = nil
      begin
        res = yield self
        exec("COMMIT")
        in_tx = false
        res
      rescue PG::RollbackTransaction
        in_tx = false
        exec("ROLLBACK")
        nil
      rescue Exception
        in_tx = false
        begin
          exec("ROLLBACK")
        rescue Exception
        end
        raise
      ensure
        if in_tx
          begin
            if Thread.current.status == "aborting"
              exec("ROLLBACK")
            else
              exec("COMMIT")
            end
          rescue Exception
          end
        end
      end
    end

    def conndefaults
      self.class.conndefaults
    end

    def conndefaults_hash
      self.class.conndefaults_hash
    end

    def conninfo
      [
        { keyword: "host", envvar: "PGHOST", compiled: nil, val: host, label: "Database-host", dispchar: "", dispsize: 40 },
        { keyword: "port", envvar: "PGPORT", compiled: "5432", val: port.to_s, label: "Database-port", dispchar: "", dispsize: 6 },
        { keyword: "dbname", envvar: "PGDATABASE", compiled: nil, val: db, label: "Database-name", dispchar: "", dispsize: 20 },
        { keyword: "user", envvar: "PGUSER", compiled: nil, val: user, label: "Database-user", dispchar: "", dispsize: 20 },
        { keyword: "password", envvar: "PGPASSWORD", compiled: nil, val: pass.empty? ? nil : pass, label: "Database-password", dispchar: "*", dispsize: 20 },
        { keyword: "connect_timeout", envvar: "PGCONNECT_TIMEOUT", compiled: nil, val: __connect_timeout, label: "Connect-timeout", dispchar: "", dispsize: 10 },
        { keyword: "service", envvar: "PGSERVICE", compiled: nil, val: __service_name, label: "Database-service", dispchar: "", dispsize: 20 },
      ]
    end

    def conninfo_hash
      h = {
        host: host,
        port: port.to_s,
        dbname: db,
        user: user,
        password: pass.empty? ? nil : pass,
        connect_timeout: __connect_timeout,
      }
      svc = __service_name
      h[:service] = svc if svc
      h
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

    def self.__build_detached(keys, vals)
      t = allocate
      t.__init_detached(keys, vals)
      t
    end

    def __init(result, index, keys)
      @result = result
      @index = index
      @keys = keys
      @values = Array.new(keys.length, UNSET)
    end

    def __init_detached(keys, vals)
      @result = nil
      @index = 0
      @keys = keys
      @values = vals
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
      @keys.rindex(key)
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
      pairs = @keys.each_with_index.map do |k, i|
        ks = k.is_a?(Symbol) ? k.to_s : k.to_s
        "#{ks}: #{__val(i).inspect}"
      end
      "#<PG::Tuple #{pairs.join(', ')}>"
    end

    INTERNAL_IVARS = [:@result, :@index, :@keys, :@values].freeze
    private_constant :INTERNAL_IVARS

    def marshal_dump
      extras = {}
      instance_variables.each do |iv|
        next if INTERNAL_IVARS.include?(iv)
        extras[iv] = instance_variable_get(iv)
      end
      extras.empty? ? [@keys, values] : [@keys, values, extras]
    end

    def marshal_load(ary)
      @result = nil
      @index = nil
      @keys, @values, extras = ary
      if extras.is_a?(Hash)
        extras.each { |iv, v| instance_variable_set(iv, v) }
      end
    end
  end

  class Result
    class << self
      undef_method :new
      undef_method :allocate
    end

    def inspect
      if cleared?
        "#<#{self.class}:0x#{object_id.to_s(16)} cleared>"
      else
        "#<#{self.class}:0x#{object_id.to_s(16)} status=#{res_status} tuples=#{ntuples} fields=#{nfields}>"
      end
    end
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
      raise TypeError, "no implicit conversion of #{index.class} into Integer" unless index.is_a?(Integer)
      raise IndexError, "Index #{index} is out of range" if index < 0 || index >= ntuples
      ks = field_name_type == :symbol ? fields.map(&:to_sym) : fields
      Tuple.__build(self, index, ks)
    end

    def each
      return enum_for(:each) { ntuples } unless block_given?
      ntuples.times { |i| yield self[i] }
      self
    end

    def each_row
      return enum_for(:each_row) { ntuples } unless block_given?
      ntuples.times { |i| yield tuple_values(i) }
      self
    end

    def each_tuple
      return enum_for(:each_tuple) { ntuples } unless block_given?
      ntuples.times { |i| yield tuple(i) }
      self
    end

    def __check_streamable!
      raise FrozenError, "can't modify frozen PG::Result" if frozen?
      check if result_status == PGRES_FATAL_ERROR
      st = result_status
      if st != PGRES_SINGLE_TUPLE && !__single_row?
        raise PG::InvalidResultStatus, "result is not in single row mode"
      end
    end
    private :__check_streamable!

    def stream_each
      __check_streamable!
      return enum_for(:stream_each) unless block_given?
      while result_status == PGRES_SINGLE_TUPLE
        yield self[0]
        __advance_single_row
        check if result_status == PGRES_FATAL_ERROR
      end
      self
    end

    def stream_each_row
      __check_streamable!
      return enum_for(:stream_each_row) unless block_given?
      while result_status == PGRES_SINGLE_TUPLE
        yield tuple_values(0)
        __advance_single_row
        check if result_status == PGRES_FATAL_ERROR
      end
      self
    end

    def stream_each_tuple
      __check_streamable!
      return enum_for(:stream_each_tuple) unless block_given?
      return self unless result_status == PGRES_SINGLE_TUPLE
      ks = field_name_type == :symbol ? fields.map(&:to_sym) : fields
      begin
        while result_status == PGRES_SINGLE_TUPLE
          yield Tuple.__build_detached(ks, tuple_values(0))
          __advance_single_row
          check if result_status == PGRES_FATAL_ERROR
        end
      rescue Exception
        clear unless cleared?
        raise
      end
      self
    end

    def field_values(name)
      idx = fnumber(name)
      raise IndexError, "no such field #{name.inspect}" if idx < 0
      column_values(idx)
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
