module PP
  def self.pp(object, out = $stdout)
    out.write(object.inspect)
    out.write("\n")
    out
  end
end