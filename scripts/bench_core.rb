# Micro-benchmarks of core methods on hot paths, as a regression check for
# Ruby-level overrides in gem/core_ext (a Ruby override of a builtin
# operator, or a slow port of a C method, shows here as a several-fold
# slowdown). Run on an idle machine:
#
#   bin/oruby scripts/bench_core.rb        # oruby
#   ruby scripts/bench_core.rb             # MRI, for reference
#
# Seconds per row, oruby on 2026-11-19 (Apple Silicon), after go-fast.c:
#   int while ==  0.046   Float#round(2) 0.005   Integer#to_s 0.007
#   rand          0.049   each_with_index 0.010  map(&:to_s)  0.029
#   range map/select 0.048  hash merge/select 0.034  Array#sum 0.000
def bm(name)
  t = Process.clock_gettime(Process::CLOCK_MONOTONIC)
  yield
  printf("%-24s %.3f\n", name, Process.clock_gettime(Process::CLOCK_MONOTONIC) - t)
end

N = 60_000
bm("int while ==")        { i = 0; n = 0; while i < 2_000_000; n += 1 if i % 7 == 3; i += 1; end }
bm("times/upto")          { s = 0; 300.times { |a| 1.upto(1000) { |b| s += b } } }
bm("range map/select")    { 10.times { (1..N).map { |x| x * 2 }.select(&:even?) } }
bm("range inject/sum")    { 10.times { (1..N).inject(:+); (1..N).sum; (1..N).each_with_index.count } }
bm("array map(&:to_s)")   { a = (1..N).to_a; 10.times { a.map(&:to_s) } }
bm("enum each_slice")     { 10.times { (1..N).each_slice(3).to_a } }
bm("hash merge/select")   { h = {}; 20_000.times { |i| h[i] = i }; 20.times { h.merge(h).select { |k, v| v.odd? } } }
bm("set add/include?")    { s = Set.new; 50_000.times { |i| s << i }; 50_000.times { |i| s.include?(i) } }
bm("struct access")       { pt = Struct.new(:x, :y); a = 50_000.times.map { |i| pt.new(i, i) }; a.each { |p| p.x + p.y } }
bm("Float#round(2)")      { 100_000.times { |i| (i / 7.0).round(2) } }
bm("Float#round")         { 100_000.times { |i| (i / 7.0).round } }
bm("Float#to_s")          { 100_000.times { |i| (i / 7.0).to_s } }
bm("Integer#to_s")        { 100_000.times { |i| i.to_s } }
bm("Symbol#==")           { 100_000.times { :a == :b } }
bm("Symbol#to_proc call") { a = (1..1000).to_a; 100.times { a.map(&:succ) } }
bm("Array#sum")           { a = (1..N).to_a; 10.times { a.sum } }
bm("Range#each_with_index") { 10.times { (1..N).each_with_index { |x, i| } } }
bm("loop/break")          { 200_000.times { i = 0; loop { i += 1; break if i > 2 } } }
bm("each_with_index arr") { a = (1..50_000).to_a; 10.times { a.each_with_index { |x, i| } } }
bm("map.with_index")      { a = (1..50_000).to_a; 5.times { a.map.with_index { |x, i| x + i } } }
bm("each_slice arr")      { a = (1..50_000).to_a; 5.times { a.each_slice(2).map(&:first) } }
bm("Enumerator#next")     { e = (1..20_000).each; 20_000.times { e.next } }
bm("rand/srand")          { srand(1); 200_000.times { rand(100) } }
bm("each_with_object")    { 10.times { (1..20_000).each_with_object([]) { |x, a| a << x } } }
