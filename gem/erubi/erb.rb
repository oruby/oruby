ERB.const_set(:BINDING_FACTORY, proc { binding })

class ERB
  def result_with_hash(variables)
    raise TypeError, "expected a Hash" unless variables.is_a?(Hash)

    scope = BINDING_FACTORY.call
    variables.each { |name, value| scope.local_variable_set(name, value) }
    result(scope)
  end
end