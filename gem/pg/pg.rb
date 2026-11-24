module PG
  class Error
    attr_reader :connection, :result
  end

  def self.require_bigdecimal_without_warning
    require "bigdecimal"
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
    def self.open(*args, **kw)
      conn = kw.empty? ? new(*args) : new(*args, **kw)
      return conn unless block_given?
      begin
        yield conn
      ensure
        conn.close unless conn.finished?
      end
    end

    class << self
      alias connect open
      alias isthreadsafe threadsafe? if respond_to?(:threadsafe?)

      def quote_ident(str)
        s = str.to_s
        '"' + s.gsub('"', '""') + '"'
      end

      def escape_string(str)
        str.to_s.gsub("'", "''")
      end
      alias escape escape_string
    end

    alias sync_exec exec
    alias async_exec exec
    alias sync_exec_params exec_params
    alias async_exec_params exec_params
    alias sync_exec_prepared exec_prepared
    alias async_exec_prepared exec_prepared

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

    def conninfo_hash
      {
        host: host,
        port: port.to_s,
        dbname: db,
        user: user,
      }
    end
  end

  class Result
    def self.res_status(status)
      case status
      when PGRES_EMPTY_QUERY    then "PGRES_EMPTY_QUERY"
      when PGRES_COMMAND_OK     then "PGRES_COMMAND_OK"
      when PGRES_TUPLES_OK      then "PGRES_TUPLES_OK"
      when PGRES_COPY_OUT       then "PGRES_COPY_OUT"
      when PGRES_COPY_IN        then "PGRES_COPY_IN"
      when PGRES_BAD_RESPONSE   then "PGRES_BAD_RESPONSE"
      when PGRES_NONFATAL_ERROR then "PGRES_NONFATAL_ERROR"
      when PGRES_FATAL_ERROR    then "PGRES_FATAL_ERROR"
      else "unknown result status"
      end
    end

    def res_status(status = result_status)
      self.class.res_status(status)
    end

    def each
      return to_enum(:each) unless block_given?
      ntuples.times { |i| yield self[i] }
      self
    end

    def each_row
      return to_enum(:each_row) unless block_given?
      ntuples.times { |i| yield tuple_values(i) }
      self
    end

    def field_values(name)
      idx = fnumber(name)
      raise IndexError, "no such field #{name.inspect}" if idx < 0
      column_values(idx)
    end

    def binary_tuples
      0
    end

    def check
      if result_status == PGRES_FATAL_ERROR || result_status == PGRES_BAD_RESPONSE
        raise PG::Error, error_message
      end
      self
    end
    alias check_result check
  end

  class TypeMap
    def __decode(val, _oid, _format)
      val
    end
  end

  class TypeMapAllStrings < TypeMap
  end

  class TypeMapByClass < TypeMap
  end

  class TypeMapByColumn < TypeMap
    def initialize(coders = [])
      @coders = coders
    end
  end

  class TypeMapByMriType < TypeMap
  end

  class TypeMapInRuby < TypeMap
  end

  class BasicTypeRegistry
    def initialize
      @coders = {}
    end
  end

  class TypeMapByOid < TypeMap
    def initialize
      @decoders = {}
    end

    def add_coder(oid, &block)
      @decoders[oid] = block
    end

    def __decode(val, oid, fmt)
      if (dec = @decoders[oid])
        dec.call(val, fmt)
      else
        val
      end
    end
  end

  class BasicTypeMapForResults < TypeMapByOid
    def initialize(_conn = nil)
      super()
      add_coder(16) { |v, _| v == "t" || v == "true" || v == "1" } # bool
      add_coder(20) { |v, _| v.to_i }                              # int8
      add_coder(21) { |v, _| v.to_i }                              # int2
      add_coder(23) { |v, _| v.to_i }                              # int4
      add_coder(26) { |v, _| v.to_i }                              # oid
      add_coder(700) { |v, _| v.to_f }                             # float4
      add_coder(701) { |v, _| v.to_f }                             # float8
      add_coder(17) { |v, _| PG::Connection.unescape_bytea(v) }    # bytea
    end
  end

  class BasicTypeMapForQueries < TypeMap
    def initialize(_conn = nil)
    end
  end
end
