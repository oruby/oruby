require 'fileutils'
class Dir
  def self.tmpdir
    [ENV["TMPDIR"], ENV["TMP"], ENV["TEMP"], __systmpdir, "/tmp", "."].each do |d|
      next if d.nil? || d.empty?
      return File.expand_path(d) if File.directory?(d) && File.writable?(d)
    end
    "."
  end

  def self.mktmpdir(prefix_suffix = nil, tmpdir = nil, **)
    prefix, suffix = case prefix_suffix
                     when nil then ["d", ""]
                     when String then [prefix_suffix, ""]
                     when Array then [prefix_suffix[0].to_s, prefix_suffix[1].to_s]
                     else raise ArgumentError, "unexpected prefix_suffix: #{prefix_suffix.inspect}"
                     end
    t = Time.now.strftime("%Y%m%d") rescue Time.now.to_i.to_s
    path = __mkdtemp(tmpdir || self.tmpdir, "#{prefix}#{t}-#{$$}-*#{suffix}")
    if block_given?
      begin
        yield path.dup
      ensure
        __rm_rf(path) if File.directory?(path)
      end
    else
      path
    end
  end
end
