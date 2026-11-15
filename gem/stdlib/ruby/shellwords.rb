module Shellwords
  def self.shellsplit(line)
    __split(line)
  end

  def self.shellescape(str)
    str = str.to_s
    return "''" if str.empty?
    str = str.gsub(/[^A-Za-z0-9_\-.,:+\/@\n]/) { |m| "\\" + m }
    str.gsub(/\n/, "'\n'")
  end

  def self.shelljoin(array)
    array.map { |arg| shellescape(arg) }.join(' ')
  end

  class << self
    alias shellwords shellsplit
    alias split shellsplit
    alias escape shellescape
    alias join shelljoin
  end

  private

  def shellsplit(line) = Shellwords.__split(line)
  def shellwords(line) = Shellwords.__split(line)
  def shellescape(str) = Shellwords.escape(str)
  def shelljoin(ary) = Shellwords.join(ary)
end

class String
  def shellsplit = Shellwords.shellsplit(self)
  def shellescape = Shellwords.shellescape(self)
end

class Array
  def shelljoin = Shellwords.shelljoin(self)
end
