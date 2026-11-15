require "prettyprint"

class PP < PrettyPrint
  def self.pp(object, out = $stdout, width = 79)
    printer = new(out, width)
    printer.pp(object)
    printer.flush
    out.respond_to?(:write) ? out.write("\n") : out << "\n"
    out
  end

  def pp(object)
    @active ||= {}
    if object.is_a?(Array)
      id = object.object_id
      if @active[id]
        text("[...]")
        return self
      end
      @active[id] = true
      begin
        group(1, "[", "]") do
          seplist(object, proc { text(","); breakable }) { |value| pp(value) }
        end
      ensure
        @active.delete(id)
      end
    elsif object.respond_to?(:pretty_print)
      object.pretty_print(self)
    else
      text(object.inspect)
    end
    self
  end
end