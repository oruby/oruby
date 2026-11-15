module Etc
  VERSION = "1.4.5"

  Passwd = Struct.new(:name, :passwd, :uid, :gid, :gecos, :dir, :shell, :change, :uclass, :expire)
  Group = Struct.new(:name, :passwd, :gid, :mem)

  class Passwd
    include Enumerable

    def self.each(&block)
      return enum_for(:each) unless block
      Etc.passwd(&block)
      self
    end
  end

  class Group
    include Enumerable

    def self.each(&block)
      return enum_for(:each) unless block
      Etc.group(&block)
      self
    end
  end

  def self.__pw(a)
    return nil unless a
    extra = a.pop || []
    Passwd.new(*a, *extra)
  end

  def self.__gr(a)
    a && Group.new(*a)
  end

  def self.getlogin
    __getlogin
  end

  def self.getpwnam(name)
    __pw(__getpwnam(name)) or raise ArgumentError, "can't find user for #{name}"
  end

  def self.getpwuid(uid = Process.uid)
    __pw(__getpwuid(uid)) or raise ArgumentError, "can't find user for #{uid}"
  end

  def self.getgrnam(name)
    __gr(__getgrnam(name)) or raise ArgumentError, "can't find group for #{name}"
  end

  def self.getgrgid(gid = Process.gid)
    __gr(__getgrgid(gid)) or raise ArgumentError, "can't find group for #{gid}"
  end

  def self.getpwent
    __pw(__getpwent)
  end

  def self.getgrent
    __gr(__getgrent)
  end

  def self.passwd
    if block_given?
      raise RuntimeError, "parallel passwd iteration" if @__in_passwd
      @__in_passwd = true
      begin
        setpwent
        while (pw = getpwent)
          yield pw
        end
      ensure
        endpwent
        @__in_passwd = false
      end
      nil
    else
      getpwent
    end
  end

  def self.group
    if block_given?
      raise RuntimeError, "parallel group iteration" if @__in_group
      @__in_group = true
      begin
        setgrent
        while (gr = getgrent)
          yield gr
        end
      ensure
        endgrent
        @__in_group = false
      end
      nil
    else
      getgrent
    end
  end

  def self.sysconfdir
    "/etc"
  end
end
