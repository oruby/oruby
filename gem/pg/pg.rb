module PG
  class Error
    attr_reader :connection, :result
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
    end

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
