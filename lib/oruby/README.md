# Vendored Ruby libraries

This tree is ORuby's install layout (`<prefix>/lib/oruby`); `make bin` builds
the tools into `bin/` beside it, so they run in-tree, and `make install
PREFIX=…` copies both.

| Path | Contents |
|------|----------|
| `4.1.0/rubygems.rb`, `4.1.0/rubygems/` | RubyGems 4.1.0.beta2 |
| `4.1.0/bundler.rb`, `4.1.0/bundler/` | Bundler 4.1.0.beta2 |
| `gems/4.1.0/specifications/default/` | MRI 4.1's default gem specifications (bundler, json, …) |

The sources are unmodified copies of those shipped with MRI 4.1.0-preview1
(`lib/ruby/4.1.0+5` and its `specifications/default`); they come from the
`ruby/rubygems` repository, tag `v4.1.0.beta2`. Update them by copying a newer
MRI's files over these, not by editing them.

Licenses: RubyGems is distributed under the Ruby license or the MIT license,
Bundler under the MIT license; both texts, from `ruby/rubygems` at
`v4.1.0.beta2`, are in `licenses/`. The libraries vendored inside them
(`bundler/vendor`, `rubygems/vendor`) carry their own upstream licenses.
