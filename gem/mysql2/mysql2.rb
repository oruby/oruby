module Mysql2
  class Error
    attr_reader :error_number, :sql_state
    alias errno error_number
  end

  class Client
    @@default_query_options = {
      as: :hash,
      async: false,
      cast_booleans: false,
      symbolize_keys: false,
      database_timezone: :local,
      application_timezone: nil,
      cache_rows: true,
      connect_flags: FOUND_ROWS,
      cast: true,
    }

    def self.default_query_options
      @@default_query_options
    end

    def query_options
      @query_options ||= @@default_query_options.dup
    end

    def warning_count
      0
    end

    def more_results?
      false
    end

    def next_result
      false
    end

    def abandon_results!
      nil
    end
  end
end
