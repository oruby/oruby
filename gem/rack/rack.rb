module ORuby
  module Rack
    def self.__collect_body(body)
      chunks = []
      begin
        if body.respond_to?(:each)
          body.each { |chunk| chunks << chunk.to_s }
        elsif body.respond_to?(:call)
          # Rack 3 streaming callable body
          stream = Object.new
          stream.define_singleton_method(:write) { |chunk| chunks << chunk.to_s; chunk.to_s.bytesize }
          stream.define_singleton_method(:<<) { |chunk| write(chunk); self }
          stream.define_singleton_method(:flush) { self }
          stream.define_singleton_method(:close) { nil }
          body.call(stream)
        end
      ensure
        body.close if body.respond_to?(:close)
      end
      chunks
    end
  end
end

module Rack
  module Handler
    module ORuby
      def self.run(app, **options)
        srv = ::ORuby::Rack::Server.new(app, options)
        yield srv if block_given?
        srv.run
      end

      def self.valid_options
        {
          "Host=HOST" => "Hostname to listen on (default: 127.0.0.1)",
          "Port=PORT" => "Port to listen on (default: 9292)",
        }
      end
    end
  end
end

module Rackup
  module Handler
    ORuby = ::Rack::Handler::ORuby
    if respond_to?(:register)
      register :oruby, ORuby
    end
  end
end
