module RbConfig
  def self.expand(val, config = CONFIG)
    return val unless val.include?('$')
    newval = val.gsub(/\$\$|\$\(([^()]+)\)|\$\{([^{}]+)\}/) do
      var = $&
      if !(v = $1 || $2)
        '$'
      elsif v =~ /\A([^:]+)(?::(.*?)=(.*))?\z/ && (key = config[v = $1])
        pat, sub = $2, $3
        config[v] = false
        config[v] = expand(key, config)
        key = key.gsub(/#{Regexp.quote(pat)}(\s|\z)/n) { sub + $1 } if pat
        key
      else
        var
      end
    end
    val.replace(newval) unless newval == val
    val
  end

  CONFIG.each_value { |v| expand(v) if String === v && v.include?('$') }
end
