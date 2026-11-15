module Find
  def self.find(*paths, ignore_error: true)
    return to_enum(:find, *paths) unless block_given?
    paths.collect! do |d|
      raise Errno::ENOENT, d unless File.exist?(d)
      d.dup
    end.each do |path|
      ps = [path]
      while (file = ps.shift)
        catch(:prune) do
          yield file.dup
          begin
            fs = __children(file)
          rescue Errno::ENOENT, Errno::EACCES, Errno::ENOTDIR, Errno::ELOOP, Errno::ENAMETOOLONG => __exc
            raise __exc unless ignore_error
            next
          end
          if fs
            fs.reverse_each do |f|
              ps.unshift(File.join(file, f))
            end
          end
        end
      end
    end
    nil
  end

  def self.prune
    throw :prune
  end

  def find(*paths, ignore_error: true, &block)
    Find.find(*paths, ignore_error: ignore_error, &block)
  end

  def prune
    Find.prune
  end
end
