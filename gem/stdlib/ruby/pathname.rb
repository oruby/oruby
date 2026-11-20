class Pathname
  VERSION = "0.4.0"

  SEPARATOR_LIST = "/"
  SEPARATOR_PAT = /\//
  TO_PATH = :to_path

  def initialize(path)
    path = path.to_path if path.respond_to?(:to_path)
    raise TypeError, "no implicit conversion of #{path.class} into String" unless path.is_a?(String)
    raise ArgumentError, "pathname contains null byte" if path.include?("\0")
    @path = path.dup
  end

  def self.getwd
    new(Dir.getwd)
  end

  def self.pwd
    new(Dir.getwd)
  end

  def self.glob(*args, **kw)
    if block_given?
      Dir.glob(*args, **kw) { |f| yield new(f) }
      nil
    else
      Dir.glob(*args, **kw).map { |f| new(f) }
    end
  end

  def self.mktmpdir(*args, &block)
    require "tmpdir"
    if block
      Dir.mktmpdir(*args) { |dir| yield new(dir) }
    else
      new(Dir.mktmpdir(*args))
    end
  end

  def to_s
    @path.dup
  end
  alias to_path to_s

  def inspect
    "#<#{self.class}:#{@path}>"
  end

  def freeze
    @path.freeze
    super
  end

  def ==(other)
    return false unless Pathname === other
    other.to_s == @path
  end
  alias === ==
  alias eql? ==

  def <=>(other)
    return nil unless Pathname === other
    @path.tr("/", "\0") <=> other.to_s.tr("/", "\0")
  end

  def hash
    @path.hash
  end

  def sub(*args, &block)
    if block
      self.class.new(@path.sub(*args) { |*m| block.call(*m) })
    else
      self.class.new(@path.sub(*args))
    end
  end

  def sub_ext(repl)
    ext = File.extname(@path)
    self.class.new(@path.chomp(ext) + repl)
  end

  def realpath(basedir = nil)
    self.class.new(File.realpath(@path, basedir))
  end

  def realdirpath(basedir = nil)
    self.class.new(File.realdirpath(@path, basedir))
  end

  # -- pure path operations ---------------------------------------------------

  def chop_basename(path)
    base = File.basename(path)
    if /\A\/?\z/ =~ base
      nil
    else
      [path[0, path.rindex(base)], base]
    end
  end
  private :chop_basename

  def split_names(path)
    names = []
    while (r = chop_basename(path))
      path, basename = r
      names.unshift basename
    end
    [path, names]
  end
  private :split_names

  def prepend_prefix(prefix, relpath)
    if relpath.empty?
      File.dirname(prefix)
    elsif /\// =~ prefix
      prefix = File.dirname(prefix)
      prefix = File.join(prefix, "") if File.basename(prefix + "a") != "a"
      prefix + relpath
    else
      prefix + relpath
    end
  end
  private :prepend_prefix

  def cleanpath(consider_symlink = false)
    if consider_symlink
      cleanpath_conservative
    else
      cleanpath_aggressive
    end
  end

  def cleanpath_aggressive
    path = @path
    names = []
    pre = path
    while (r = chop_basename(pre))
      pre, base = r
      case base
      when "."
      when ".."
        names.unshift base
      else
        if names[0] == ".."
          names.shift
        else
          names.unshift base
        end
      end
    end
    pre = pre.tr("\\", "/") if File::ALT_SEPARATOR rescue nil
    if /\// =~ File.basename(pre)
      names.shift while names[0] == ".."
    end
    self.class.new(prepend_prefix(pre, File.join(*names)))
  end
  private :cleanpath_aggressive

  def has_trailing_separator?(path)
    if (r = chop_basename(path))
      pre, basename = r
      pre.length + basename.length < path.length
    else
      false
    end
  end
  private :has_trailing_separator?

  def add_trailing_separator(path)
    if File.basename(path + "a") == "a"
      path
    else
      File.join(path, "")
    end
  end
  private :add_trailing_separator

  def del_trailing_separator(path)
    if (r = chop_basename(path))
      pre, basename = r
      pre + basename
    elsif /\/+\z/ =~ path
      $` + File.dirname(path)[/\/*\z/]
    else
      path
    end
  end
  private :del_trailing_separator

  def cleanpath_conservative
    path = @path
    names = []
    pre = path
    while (r = chop_basename(pre))
      pre, base = r
      names.unshift base if base != "."
    end
    if /\// =~ File.basename(pre)
      names.shift while names[0] == ".."
    end
    if names.empty?
      self.class.new(File.dirname(pre))
    else
      if names.last != ".." && File.basename(path) == "."
        names << "."
      end
      result = prepend_prefix(pre, File.join(*names))
      if /\A(?:\.|\.\.)\z/ !~ names.last && has_trailing_separator?(path)
        self.class.new(add_trailing_separator(result))
      else
        self.class.new(result)
      end
    end
  end
  private :cleanpath_conservative

  def parent
    self + ".."
  end

  def mountpoint?
    begin
      stat1 = lstat
      stat2 = parent.lstat
      stat1.dev != stat2.dev || stat1.ino == stat2.ino
    rescue Errno::ENOENT
      false
    end
  end

  def root?
    chop_basename(@path).nil? && /\// =~ @path ? true : false
  end

  def absolute?
    @path.start_with?("/")
  end

  def relative?
    !absolute?
  end

  def each_filename
    return to_enum(__method__) unless block_given?
    _, names = split_names(@path)
    names.each { |filename| yield filename }
    nil
  end

  def descend
    return to_enum(__method__) unless block_given?
    vs = []
    ascend { |v| vs << v }
    vs.reverse_each { |v| yield v }
    nil
  end

  def ascend
    return to_enum(__method__) unless block_given?
    path = @path
    yield self
    while (r = chop_basename(path))
      path, = r
      break if path.empty?
      yield self.class.new(del_trailing_separator(path))
    end
  end

  def +(other)
    other = Pathname.new(other) unless Pathname === other
    Pathname.new(plus(@path, other.to_s))
  end
  alias / +

  def plus(path1, path2)
    prefix2 = path2
    index_list2 = []
    basename_list2 = []
    while (r2 = chop_basename(prefix2))
      prefix2, basename2 = r2
      index_list2.unshift prefix2.length
      basename_list2.unshift basename2
    end
    return path2 if prefix2 != ""
    prefix1 = path1
    while true
      while !basename_list2.empty? && basename_list2.first == "."
        index_list2.shift
        basename_list2.shift
      end
      break unless (r1 = chop_basename(prefix1))
      prefix1, basename1 = r1
      next if basename1 == "."
      if basename1 == ".." || basename_list2.empty? || basename_list2.first != ".."
        prefix1 = prefix1 + basename1
        break
      end
      index_list2.shift
      basename_list2.shift
    end
    r1 = chop_basename(prefix1)
    if !r1 && /\// =~ File.basename(prefix1)
      while !basename_list2.empty? && basename_list2.first == ".."
        index_list2.shift
        basename_list2.shift
      end
    end
    if !basename_list2.empty?
      suffix2 = path2[index_list2.first..-1]
      r1 ? File.join(prefix1, suffix2) : prefix1 + suffix2
    else
      r1 ? prefix1 : File.dirname(prefix1)
    end
  end
  private :plus

  def join(*args)
    return self if args.empty?
    result = args.pop
    result = Pathname.new(result) unless Pathname === result
    return result if result.absolute?
    args.reverse_each do |arg|
      arg = Pathname.new(arg) unless Pathname === arg
      result = arg + result
      return result if result.absolute?
    end
    self + result
  end

  def children(with_directory = true)
    with_directory = false if @path == "."
    result = []
    Dir.foreach(@path) do |e|
      next if e == "." || e == ".."
      if with_directory
        result << self.class.new(File.join(@path, e))
      else
        result << self.class.new(e)
      end
    end
    result
  end

  def each_child(with_directory = true, &b)
    children(with_directory).each(&b)
  end

  def relative_path_from(base_directory)
    base_directory = Pathname.new(base_directory) unless base_directory.is_a?(Pathname)
    dest_directory = cleanpath.to_s
    base_directory = base_directory.cleanpath.to_s
    dest_prefix = dest_directory
    dest_names = []
    while (r = chop_basename(dest_prefix))
      dest_prefix, basename = r
      dest_names.unshift basename if basename != "."
    end
    base_prefix = base_directory
    base_names = []
    while (r = chop_basename(base_prefix))
      base_prefix, basename = r
      base_names.unshift basename if basename != "."
    end
    unless dest_prefix == base_prefix
      raise ArgumentError, "different prefix: #{dest_prefix.inspect} and #{base_directory.inspect}"
    end
    while !dest_names.empty? && !base_names.empty? && dest_names.first == base_names.first
      dest_names.shift
      base_names.shift
    end
    if base_names.include?("..")
      raise ArgumentError, "base_directory has ..: #{base_directory}"
    end
    base_names.fill("..")
    relpath_names = base_names + dest_names
    if relpath_names.empty?
      Pathname.new(".")
    else
      Pathname.new(File.join(*relpath_names))
    end
  end

  # -- IO / File / Dir / FileTest delegation -----------------------------------

  def each_line(*args, &block)
    File.foreach(@path, *args, &block)
  end

  def read(*args)
    File.read(@path, *args)
  end

  def binread(*args)
    File.binread(@path, *args)
  end

  def readlines(*args)
    File.readlines(@path, *args)
  end

  def write(*args, **kw)
    File.write(@path, *args, **kw)
  end

  def binwrite(*args)
    File.binwrite(@path, *args)
  end

  def sysopen(*args)
    IO.sysopen(@path, *args)
  end

  def atime; File.atime(@path); end
  def birthtime; File.birthtime(@path); end
  def ctime; File.ctime(@path); end
  def mtime; File.mtime(@path); end
  def chmod(mode); File.chmod(mode, @path); end
  def lchmod(mode); File.lchmod(mode, @path); end
  def chown(owner, group); File.chown(owner, group, @path); end
  def lchown(owner, group); File.lchown(owner, group, @path); end
  def fnmatch(pattern, *args); File.fnmatch(pattern, @path, *args); end
  def fnmatch?(pattern, *args); File.fnmatch?(pattern, @path, *args); end
  def ftype; File.ftype(@path); end
  def make_link(old); File.link(old, @path); end
  def readlink; self.class.new(File.readlink(@path)); end
  def rename(to); File.rename(@path, to); end
  def stat; File.stat(@path); end
  def lstat; File.lstat(@path); end
  def make_symlink(old); File.symlink(old, @path); end
  def truncate(length); File.truncate(@path, length); end
  def utime(atime, mtime); File.utime(atime, mtime, @path); end
  def basename(*args); self.class.new(File.basename(@path, *args)); end
  def dirname; self.class.new(File.dirname(@path)); end
  def extname; File.extname(@path); end
  def expand_path(*args); self.class.new(File.expand_path(@path, *args)); end
  def split; File.split(@path).map { |f| self.class.new(f) }; end

  def open(*args, **kw, &block)
    File.open(@path, *args, **kw, &block)
  end

  def blockdev?; FileTest.blockdev?(@path); end
  def chardev?; FileTest.chardev?(@path); end
  def executable?; FileTest.executable?(@path); end
  def executable_real?; FileTest.executable_real?(@path); end
  def exist?; FileTest.exist?(@path); end
  def grpowned?; FileTest.grpowned?(@path); end
  def directory?; FileTest.directory?(@path); end
  def file?; FileTest.file?(@path); end
  def pipe?; FileTest.pipe?(@path); end
  def socket?; FileTest.socket?(@path); end
  def owned?; FileTest.owned?(@path); end
  def readable?; FileTest.readable?(@path); end
  def world_readable?; File.world_readable?(@path); end
  def readable_real?; FileTest.readable_real?(@path); end
  def setuid?; FileTest.setuid?(@path); end
  def setgid?; FileTest.setgid?(@path); end
  def size; FileTest.size(@path); end
  def size?; FileTest.size?(@path); end
  def sticky?; FileTest.sticky?(@path); end
  def symlink?; FileTest.symlink?(@path); end
  def writable?; FileTest.writable?(@path); end
  def world_writable?; File.world_writable?(@path); end
  def writable_real?; FileTest.writable_real?(@path); end
  def zero?; FileTest.zero?(@path); end

  def empty?
    if FileTest.directory?(@path)
      Dir.empty?(@path)
    else
      FileTest.empty?(@path)
    end
  end

  def glob(*args, **kw)
    if block_given?
      Dir.glob(*args, base: @path, **kw) { |f| yield self + f }
      nil
    else
      Dir.glob(*args, base: @path, **kw).map { |f| self + f }
    end
  end

  def entries
    Dir.entries(@path).map { |f| self.class.new(f) }
  end

  def mkdir(*args); Dir.mkdir(@path, *args); end
  def rmdir; Dir.rmdir(@path); end
  def opendir(&block); Dir.open(@path, &block); end

  def each_entry(&block)
    Dir.foreach(@path) { |f| yield self.class.new(f) }
  end

  def mkpath(mode: nil)
    require "fileutils"
    FileUtils.mkpath(@path, mode: mode)
    self
  end

  def rmtree(noop: nil, verbose: nil, secure: nil)
    require "fileutils"
    FileUtils.rm_rf(@path)
    self
  end

  def unlink
    if Dir.exist?(@path)
      Dir.unlink(@path)
    else
      File.unlink(@path)
    end
  end
  alias delete unlink

  def find(ignore_error: true)
    return to_enum(__method__, ignore_error: ignore_error) unless block_given?
    require "find"
    if @path == "."
      Find.find(@path, ignore_error: ignore_error) { |f| yield self.class.new(f.sub(%r{\A\./}, "")) }
    else
      Find.find(@path, ignore_error: ignore_error) { |f| yield self.class.new(f) }
    end
  end
end

module Kernel
  def Pathname(path)
    return path if Pathname === path
    Pathname.new(path)
  end
  module_function :Pathname  # private, and Kernel.Pathname public
end
