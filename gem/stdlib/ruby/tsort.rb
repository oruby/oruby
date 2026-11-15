module TSort
  class Cyclic < StandardError
  end

  def tsort_each_node
    raise NotImplementedError, "tsort_each_node is not implemented"
  end

  def tsort_each_child(node)
    raise NotImplementedError, "tsort_each_child is not implemented"
  end

  def tsort
    result = []
    tsort_each { |node| result << node }
    result
  end

  def tsort_each(&block)
    return enum_for(:tsort_each) unless block

    visited = {}
    visiting = {}
    tsort_each_node do |node|
      tsort_visit(node, visited, visiting, &block)
    end
    self
  end

  def strongly_connected_components
    components = []
    each_strongly_connected_component { |component| components << component }
    components
  end

  def each_strongly_connected_component(&block)
    return enum_for(:each_strongly_connected_component) unless block

    indices = {}
    lowlinks = {}
    stack = []
    on_stack = {}
    tsort_each_node do |node|
      tsort_scc_visit(node, indices, lowlinks, stack, on_stack, &block)
    end
    self
  end

  private

  def tsort_visit(node, visited, visiting, &block)
    raise Cyclic, "topological sort failed at #{node.inspect}" if visiting[node]
    return if visited[node]

    visiting[node] = true
    tsort_each_child(node) do |child|
      tsort_visit(child, visited, visiting, &block)
    end
    visiting.delete(node)
    visited[node] = true
    block.call(node)
  end

  def tsort_scc_visit(node, indices, lowlinks, stack, on_stack, &block)
    return if indices.key?(node)

    indices[node] = indices.length
    lowlinks[node] = indices[node]
    stack << node
    on_stack[node] = true

    tsort_each_child(node) do |child|
      if !indices.key?(child)
        tsort_scc_visit(child, indices, lowlinks, stack, on_stack, &block)
        lowlinks[node] = [lowlinks[node], lowlinks[child]].min
      elsif on_stack[child]
        lowlinks[node] = [lowlinks[node], indices[child]].min
      end
    end

    if lowlinks[node] == indices[node]
      node_index = indices[node]
      component = []
      begin
        child = stack.pop
        on_stack.delete(child)
        component << child
      end until indices[child] == node_index
      block.call(component)
    end
  end
end