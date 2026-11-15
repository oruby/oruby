module OpenURI
  def self.open_uri(name, *options, **keywords)
    stream = __open(name, *options, **keywords)
    return stream unless block_given?

    begin
      yield stream
    ensure
      stream.close unless stream.closed?
    end
  end

  class << self
    alias open open_uri
  end
end

module URI
  def self.open(name, *options, **keywords, &block)
    OpenURI.open_uri(name, *options, **keywords, &block)
  end
end