# Writes the executables MRI installs next to ruby into DIR (default bin/):
# gem, and a RubyGems stub for each executable of the gems ORuby ships
# (bundle, bundler, irb, rdoc, ri, rbs, syntax_suggest, ...).
#
#   bin/oruby scripts/gen_bin_stubs.rb [DIR]     (make bin)
#
# The stubs are RubyGems' own (Gem::Installer#app_script_text), with MRI's
# load-relative prologue (--enable-load-relative) in place of the shebang:
# /bin/sh runs the oruby beside the script with -x, which skips to the
# second #! line. So they work in-tree, installed, and moved, unchanged.
# An executable whose file the gem does not have is left out.
#
# A shipped gem whose C extension is compiled into oruby (rbs: gem/rbs) gets
# the gem.build_complete marker MRI's install leaves in its extension dir,
# which Bundler takes as built (it does not ask Gem::Specification).
require "rubygems"
require "rubygems/installer"
require "fileutils"

dir = File.expand_path(ARGV[0] || File.join(__dir__, "..", "bin"))
PROLOGUE = <<~SH
  #!/bin/sh
  # -*- ruby -*-
  _=_\\
  =begin
  bindir="${0%/*}"
  exec "$bindir/oruby" "-x" "$0" "$@"
  =end
  #!/usr/bin/env ruby
SH

def write(dir, name, body)
  path = File.join(dir, name)
  text = PROLOGUE + body
  return if File.exist?(path) && File.binread(path) == text
  File.binwrite(path, text)
  File.chmod(0o755, path)
end

written = ["gem"]
write(dir, "gem", <<~RUBY)
  # frozen_string_literal: true

  #--
  # Copyright 2006 by Chad Fowler, Rich Kilmer, Jim Weirich and others.
  # All rights reserved.
  # See LICENSE.txt for permissions.
  #++

  require "rubygems/gem_runner"

  Gem::GemRunner.new.run ARGV.clone
RUBY

real = ->(p) { File.exist?(p) ? File.realpath(p) : File.expand_path(p) }
lib_gems = real.(File.join(RbConfig::CONFIG["rubylibprefix"], "gems", RbConfig::CONFIG["ruby_version"]))
# the gems ORuby ships, whatever GEM_HOME / GEM_PATH say
Gem.use_paths(lib_gems, [lib_gems])
specs = Gem::Specification.select { |s| !s.executables.empty? && real.(s.base_dir) == lib_gems }
specs.sort_by(&:name).each do |spec|
  installer = Gem::Installer.for_spec(spec, install_dir: spec.base_dir)
  spec.executables.each do |exe|
    next if written.include?(exe) || exe == "oruby"
    next unless File.exist?(File.join(spec.gem_dir, spec.bindir, exe))
    body = installer.app_script_text(exe).sub(/\A#!.*\n/, "")
    write(dir, exe, body)
    written << exe
  end
end
puts "gen_bin_stubs: #{written.sort.join(" ")} in #{dir}"

Gem::Specification.each do |spec|
  next if spec.default_gem? || spec.extensions.empty? || real.(spec.base_dir) != lib_gems
  next if File.exist?(spec.gem_build_complete_path) || spec.missing_extensions?
  FileUtils.mkdir_p(spec.extension_dir)
  File.write(spec.gem_build_complete_path, "")
  puts "gen_bin_stubs: #{spec.full_name}'s extension is compiled in"
end
