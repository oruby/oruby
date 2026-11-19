# MRI 4.1's #source_range: where a Proc, a Method or a backtrace frame's
# code is, as a Ruby::SourceRange (lines from 1, byte columns from 0, the
# end exclusive). mruby's debug info has only lines, so the source is parsed
# again (Kernel.__oruby_prism_nodes, go-srcrange.c) and the node is picked
# among those on the line: for a frame by the instruction it was at
# (@__oruby_site, [opcode, symbol, eval?]), for a Proc or a Method by where its block or
# def starts. A file is read again when asked; code eval ran is kept
# (Kernel.__oruby_kept_sources, the last 32).
module Ruby
  class SourceRange
    attr_reader :path, :absolute_path, :start_line, :start_column, :end_line, :end_column

    class << self
      undef_method :new

      # node: a record of Kernel.__oruby_prism_nodes; off: lines before the
      # source (eval's first line - 1)
      def __oruby_new(path, abs, node, off)
        allocate.__send__(:__oruby_init, path, abs, node[1] + off, node[2], node[3] + off, node[4])
      end

      # [source, line offset, eval?] of the code at path:line; nil when it is
      # not known
      def __oruby_source(path, line, abs)
        return nil if path.nil? || path.start_with?("<internal:", "mrblib/", "mrbgems/")
        Kernel.__oruby_kept_sources.reverse_each do |f, first, src|
          next unless f ? f == path : path.start_with?("(eval")
          next if line < first || line > first + src.count("\n")
          return [src, first - 1, true]
        end
        nil
      end

      PROC_NODES = %w[BlockNode LambdaNode ForNode].freeze
      METHOD_NODES = %w[DefNode BlockNode LambdaNode].freeze

      # a Proc's or a Method's range: the block, lambda, for body or def
      # starting at line (a def named name first)
      def __oruby_of(loc, name = nil)
        return nil unless loc
        path, line = loc
        return nil if path.nil? || path.start_with?("<internal:", "mrblib/", "mrbgems/")
        kept = __oruby_source(path, line, nil)
        if kept
          src, off, = kept
          abs = nil
        else
          begin
            src = File.binread(path)
            abs = File.realpath(path)
          rescue SystemCallError
            return nil
          end
          off = 0
        end
        nodes = Kernel.__oruby_prism_nodes(src, line - off)
        return nil unless nodes
        at = nodes.select { |n| n[1] == line - off }
        node = (name && at.find { |n| n[0] == "DefNode" && n[5] == name }) ||
               at.find { |n| (name ? METHOD_NODES : PROC_NODES).include?(n[0]) }
        node && __oruby_new(path, abs, node, off)
      end

      # instructions that are a call of an operator method
      OP_CALLS = {
        ADD: :+, ADDI: :+, ADDILV: :+, SUB: :-, SUBI: :-, SUBILV: :-, MUL: :*, DIV: :/,
        EQ: :==, LT: :<, LE: :<=, GT: :>, GE: :>=, GETIDX: :[], GETIDX0: :[], SETIDX: :[]=,
      }.freeze
      SENDS = %i[SEND SEND0 SENDB SSEND SSEND0 SSENDB].freeze
      OP_WRITES = %w[LocalVariableOperatorWriteNode InstanceVariableOperatorWriteNode
                     ClassVariableOperatorWriteNode GlobalVariableOperatorWriteNode
                     ConstantOperatorWriteNode ConstantPathOperatorWriteNode].freeze
      CONST_PATH_WRITES = %w[ConstantPathOperatorWriteNode ConstantPathOrWriteNode
                             ConstantPathAndWriteNode].freeze
      CODE_NODES = %w[BlockNode LambdaNode DefNode ClassNode ModuleNode SingletonClassNode].freeze

      # the nodes a call of mid may be
      def __oruby_call_nodes(nodes, mid)
        s = mid.to_s
        if s.start_with?("__oruby_cpath_")
          return nodes.select { |n| CONST_PATH_WRITES.include?(n[0]) }
        end
        return nodes.select { |n| %w[MultiWriteNode MultiTargetNode].include?(n[0]) } if mid == :__oruby_to_ary
        return nodes.select { |n| n[0] == "KeywordHashNode" } if mid == :__oruby_kwsplat
        # pattern matching (core_ext's helpers)
        return nodes.select { |n| PATTERNS.include?(n[0]) } if s.start_with?("__pat")
        return [__oruby_splat_node(nodes) || __oruby_condition(nodes)].compact if mid == :__case_eqq
        index = mid == :[] || mid == :[]=
        found = nodes.select do |n|
          case n[0]
          when "CallNode", "CallTargetNode" then n[5] == mid
          when "IndexTargetNode" then mid == :[]=
          when "IndexOperatorWriteNode" then index || n[6] == mid
          when "IndexOrWriteNode", "IndexAndWriteNode" then index
          when "CallOperatorWriteNode" then n[5] == mid || n[7] == mid || n[6] == mid
          when "CallOrWriteNode", "CallAndWriteNode" then n[5] == mid || n[7] == mid
          when *OP_WRITES then n[6] == mid
          else false
          end
        end
        # case/when and in compare with ===: the condition (the deepest node)
        found = [__oruby_condition(nodes)].compact if found.empty? && mid == :===
        found
      end

      def __oruby_condition(nodes) = nodes.reject { |n| CONTAINERS.include?(n[0]) }.last

      PATTERNS = %w[HashPatternNode ArrayPatternNode FindPatternNode].freeze
      CONTAINERS = %w[ProgramNode StatementsNode BeginNode EnsureNode CaseNode CaseMatchNode
                      WhenNode InNode ElseNode ClassNode ModuleNode SingletonClassNode
                      DefNode BlockNode LambdaNode ParenthesesNode].freeze

      # an implicit Array of a splat (a = *v, a, b = *v) is the ArrayNode, as
      # wide as its SplatNode; the splat itself otherwise
      def __oruby_splat_node(nodes)
        splats = nodes.select { |n| n[0] == "SplatNode" }
        ary = nodes.find { |n| n[0] == "ArrayNode" && splats.any? { |sp| sp[1, 4] == n[1, 4] } }
        ary || splats.last
      end

      # the node the frame at site runs, among nodes (those on its line)
      def __oruby_pick(nodes, site, line)
        op, sym = site
        cands =
          if (mid = OP_CALLS[op]) || SENDS.include?(op)
            __oruby_call_nodes(nodes, mid || sym)
          else
            case op
            when :GETCONST
              nodes.select { |n| n[5] == sym && n[0].start_with?("Constant") && !n[0].start_with?("ConstantPath") }
            when :GETMCNST
              nodes.select { |n| n[5] == sym && (n[0] == "ConstantPathNode" || CONST_PATH_WRITES.include?(n[0])) }
            when :SETMCNST
              nodes.select { |n| n[5] == sym && %w[ConstantPathWriteNode ConstantPathTargetNode].include?(n[0]) }
            when :SETCONST
              nodes.select { |n| n[5] == sym && %w[ConstantWriteNode ConstantTargetNode].include?(n[0]) }
            when :GETCV, :SETCV
              nodes.select { |n| n[5] == sym && n[0].start_with?("ClassVariable") }
            when :ENTER, :KARG, :KEY_P, :KEYEND
              nodes.select { |n| CODE_NODES.include?(n[0]) }.last(1)
            when :CLASS then nodes.select { |n| n[0] == "ClassNode" }.last(1)
            when :MODULE then nodes.select { |n| n[0] == "ModuleNode" }.last(1)
            when :SCLASS then nodes.select { |n| n[0] == "SingletonClassNode" }.last(1)
            when :EXEC then nodes.select { |n| %w[ClassNode ModuleNode SingletonClassNode].include?(n[0]) }.last(1)
            when :ARYSPLAT, :ARYCAT
              [__oruby_splat_node(nodes)].compact
            when :HASH, :HASHADD, :HASHCAT
              nodes.select { |n| n[0] == "KeywordHashNode" || n[0] == "HashNode" }
            when :APOST
              nodes.select { |n| n[0] == "MultiWriteNode" || n[0] == "MultiTargetNode" }
            else
              []
            end
          end
        cands.first
      end
    end

    def inspect
      "#<Ruby::SourceRange #{@absolute_path || @path}:(#{@start_line},#{@start_column})-(#{@end_line},#{@end_column})>"
    end

    private

    def __oruby_init(path, abs, sl, sc, el, ec)
      @path = path
      @absolute_path = abs
      @start_line = sl
      @start_column = sc
      @end_line = el
      @end_column = ec
      self
    end
  end
end

class Proc
  def source_range
    if (m = instance_variable_get(:@__oruby_method))
      return m.source_range
    end
    Ruby::SourceRange.__oruby_of(source_location)
  end
end

class Method
  def source_range = Ruby::SourceRange.__oruby_of(source_location, original_name)
end

class UnboundMethod
  def source_range
    Ruby::SourceRange.__oruby_of(source_location, respond_to?(:original_name) ? original_name : name)
  end
end

module Kernel
  # the program run from -e or standard input (cmd/oruby)
  def self.__oruby_keep_main_source(file, src)
    @__oruby_main_source = [file, src]
    nil
  end
end

class Thread::Backtrace::Location
  def source_range
    path = self.path
    line = lineno
    site = @__oruby_site
    return nil if site.nil? || path.start_with?("<internal:", "mrblib/", "mrbgems/")
    if site[2] # code eval ran: its kept source
      kept = Ruby::SourceRange.__oruby_source(path, line, nil)
      raise ArgumentError, "cannot get source range for location in eval" unless kept
      src, off, = kept
      abs = nil
    elsif (main = Kernel.instance_variable_get(:@__oruby_main_source)) && main[0] == path
      src = main[1]
      off = 0
      abs = nil
    else
      # a core method's frame is at the code that called it
      abs = absolute_path || File.realpath(path)
      src = File.binread(abs)
      off = 0
    end
    nodes = Kernel.__oruby_prism_nodes(src, line - off)
    raise RuntimeError, "source has been modified" unless nodes
    node = Ruby::SourceRange.__oruby_pick(nodes, site, line - off)
    # case/in and case/when helpers run at the case: their node is in it
    if node.nil? && (c = nodes.select { |n| n[0] == "CaseMatchNode" || n[0] == "CaseNode" }.last)
      node = Ruby::SourceRange.__oruby_pick(Kernel.__oruby_prism_nodes(src, line - off, c[3]), site, line - off)
    end
    raise RuntimeError, "source has been modified" unless node
    Ruby::SourceRange.__oruby_new(path, abs, node, off)
  end
end
