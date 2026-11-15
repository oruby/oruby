module FileUtils
  VERSION = "1.7.3"

  @fileutils_output = $stderr
  @fileutils_label = ""

  class << self
    attr_accessor :fileutils_output

    def __list(list)
      Array(list).flatten.map { |p| p.respond_to?(:to_path) ? p.to_path : p.to_str }
    end

    def __msg(opts, msg)
      return unless opts[:verbose]
      (@fileutils_output || $stderr).puts(msg)
    end

    def pwd = Dir.pwd

    def cd(dir, verbose: nil, &block)
      __msg({ verbose: verbose }, "cd #{dir}")
      Dir.chdir(dir, &block)
    end
    alias chdir cd

    def mkdir(list, mode: nil, noop: nil, verbose: nil)
      list = __list(list)
      __msg({ verbose: verbose }, "mkdir #{mode ? '-m %03o ' % mode : ''}#{list.join(' ')}")
      return if noop
      list.each { |dir| __mkdir(dir, mode || 0777) }
    end

    def mkdir_p(list, mode: nil, noop: nil, verbose: nil)
      list = __list(list)
      __msg({ verbose: verbose }, "mkdir -p #{mode ? '-m %03o ' % mode : ''}#{list.join(' ')}")
      return list if noop
      list.each { |dir| __mkdir_p(dir, mode || 0777) }
      list
    end
    alias makedirs mkdir_p
    alias mkpath mkdir_p

    def rmdir(list, parents: nil, noop: nil, verbose: nil)
      list = __list(list)
      __msg({ verbose: verbose }, "rmdir #{parents ? '-p ' : ''}#{list.join(' ')}")
      return if noop
      list.each do |dir|
        __rmdir(dir)
        if parents
          d = File.dirname(dir)
          while d != "." && d != "/" && (Dir.empty?(d) rescue false)
            __rmdir(d)
            d = File.dirname(d)
          end
        end
      end
    end

    def rm(list, force: nil, noop: nil, verbose: nil)
      list = __list(list)
      __msg({ verbose: verbose }, "rm#{force ? ' -f' : ''} #{list.join(' ')}")
      return if noop
      list.each do |path|
        begin
          __rm(path)
        rescue SystemCallError => __exc
          raise __exc unless force
        end
      end
    end
    alias remove rm

    def rm_f(list, noop: nil, verbose: nil)
      rm(list, force: true, noop: noop, verbose: verbose)
    end
    alias safe_unlink rm_f

    def rm_r(list, force: nil, noop: nil, verbose: nil, secure: nil)
      list = __list(list)
      __msg({ verbose: verbose }, "rm -r#{force ? 'f' : ''} #{list.join(' ')}")
      return if noop
      list.each do |path|
        begin
          __rm_r(path)
        rescue SystemCallError => __exc
          raise __exc unless force
        end
      end
    end

    def rm_rf(list, noop: nil, verbose: nil, secure: nil)
      rm_r(list, force: true, noop: noop, verbose: verbose, secure: secure)
    end
    alias rmtree rm_rf

    def remove_entry(path, force = false)
      __rm_r(path.to_s)
    rescue SystemCallError => __exc
      raise __exc unless force
    end

    def remove_entry_secure(path, force = false) = remove_entry(path, force)
    def remove_file(path, force = false) = rm(path, force: force)
    def remove_dir(path, force = false) = rm_r(path, force: force)

    def __dest(src, dest)
      File.directory?(dest) ? File.join(dest, File.basename(src)) : dest
    end

    def cp(src, dest, preserve: nil, noop: nil, verbose: nil)
      srcs = __list(src)
      __msg({ verbose: verbose }, "cp#{preserve ? ' -p' : ''} #{srcs.join(' ')} #{dest}")
      return if noop
      dest = dest.to_path if dest.respond_to?(:to_path)
      srcs.each { |s| __cp(s, __dest(s, dest), !!preserve) }
    end
    alias copy cp

    def copy_file(src, dest, preserve = false, dereference = true)
      __cp(src.to_s, dest.to_s, preserve)
    end

    def cp_r(src, dest, preserve: nil, noop: nil, verbose: nil, dereference_root: true, remove_destination: nil)
      srcs = __list(src)
      __msg({ verbose: verbose }, "cp -r#{preserve ? 'p' : ''} #{srcs.join(' ')} #{dest}")
      return if noop
      dest = dest.to_path if dest.respond_to?(:to_path)
      srcs.each { |s| __cp_r(s, __dest(s, dest), !!preserve, !!dereference_root) }
    end

    def copy_entry(src, dest, preserve = false, dereference_root = false, remove_destination = false)
      __cp_r(src.to_s, dest.to_s, preserve, dereference_root)
    end

    def mv(src, dest, force: nil, noop: nil, verbose: nil, secure: nil)
      srcs = __list(src)
      __msg({ verbose: verbose }, "mv#{force ? ' -f' : ''} #{srcs.join(' ')} #{dest}")
      return if noop
      dest = dest.to_path if dest.respond_to?(:to_path)
      srcs.each do |s|
        begin
          __mv(s, __dest(s, dest))
        rescue SystemCallError => __exc
          raise __exc unless force
        end
      end
    end
    alias move mv

    def ln(src, dest, force: nil, noop: nil, verbose: nil)
      srcs = __list(src)
      __msg({ verbose: verbose }, "ln#{force ? ' -f' : ''} #{srcs.join(' ')} #{dest}")
      return if noop
      srcs.each do |s|
        d = __dest(s, dest)
        rm_f(d) if force
        __ln(s, d)
      end
    end
    alias link ln

    def ln_s(src, dest, force: nil, noop: nil, verbose: nil, relative: false, target_directory: true)
      srcs = __list(src)
      __msg({ verbose: verbose }, "ln -s#{force ? 'f' : ''} #{srcs.join(' ')} #{dest}")
      return if noop
      srcs.each do |s|
        d = __dest(s, dest)
        rm_f(d) if force
        __ln_s(s, d)
      end
    end
    alias symlink ln_s

    def ln_sf(src, dest, noop: nil, verbose: nil)
      ln_s(src, dest, force: true, noop: noop, verbose: verbose)
    end

    def touch(list, noop: nil, verbose: nil, mtime: nil, nocreate: nil)
      list = __list(list)
      __msg({ verbose: verbose }, "touch #{list.join(' ')}")
      return if noop
      list.each do |path|
        next if nocreate && !File.exist?(path)
        __touch(path, mtime && mtime.to_f)
      end
    end

    def chmod(mode, list, noop: nil, verbose: nil)
      list = __list(list)
      __msg({ verbose: verbose }, "chmod #{mode.is_a?(Integer) ? '%o' % mode : mode} #{list.join(' ')}")
      return if noop
      raise ArgumentError, "symbolic mode is not supported" unless mode.is_a?(Integer)
      list.each { |path| __chmod(mode, path) }
    end

    def chmod_R(mode, list, noop: nil, verbose: nil, force: nil)
      __list(list).each do |root|
        require 'find'
        Find.find(root) { |p| __chmod(mode, p) }
      end
    end

    def install(src, dest, mode: nil, preserve: nil, noop: nil, verbose: nil, **)
      __list(src).each do |s|
        d = __dest(s, dest)
        cp(s, d, preserve: preserve, noop: noop, verbose: verbose)
        chmod(mode, d) if mode && !noop
      end
    end

    def compare_file(a, b) = __identical(a.to_s, b.to_s)
    alias identical? compare_file
    alias cmp compare_file

    def uptodate?(new, old_list)
      return false unless File.exist?(new)
      new_time = File.mtime(new)
      Array(old_list).all? { |old| !File.exist?(old) || File.mtime(old) < new_time }
    end
  end

  # FileUtils methods are also private instance methods of including classes
  %i[pwd cd chdir mkdir mkdir_p makedirs mkpath rmdir rm remove rm_f safe_unlink rm_r rm_rf
     rmtree remove_entry remove_entry_secure remove_file remove_dir cp copy copy_file cp_r
     copy_entry mv move ln link ln_s symlink ln_sf touch chmod chmod_R install compare_file
     identical? cmp uptodate?].each do |name|
    define_method(name) { |*args, **kw, &blk| FileUtils.__send__(name, *args, **kw, &blk) }
    private name
  end

  module Verbose
    include FileUtils
  end

  module NoWrite
    include FileUtils
  end

  module DryRun
    include FileUtils
  end
end
