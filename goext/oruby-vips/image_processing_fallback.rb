# Minimal built-in ImageProcessing::Vips fallback used only when the
# image_processing gem is NOT installed on $LOAD_PATH.
require "tempfile" unless defined?(Tempfile)
require "vips"

module ImageProcessing
  class Error < StandardError; end

  class Builder
    attr_reader :options

    def initialize(options = {})
      @options = options
    end

    def source(file)
      branch(source: file)
    end

    def convert(format)
      branch(format: format.to_s.delete_prefix("."))
    end

    def loader(**opts)
      branch(loader: (@options[:loader] || {}).merge(opts))
    end

    def saver(**opts)
      branch(saver: (@options[:saver] || {}).merge(opts))
    end

    def apply(ops)
      list = ops.is_a?(Hash) ? ops.to_a : ops
      cur = self
      list.each do |name, args|
        if args.is_a?(Array)
          cur = cur.public_send(name, *args)
        elsif args == true
          cur = cur.public_send(name)
        elsif args.is_a?(Hash)
          cur = cur.public_send(name, **args)
        else
          cur = cur.public_send(name, args)
        end
      end
      cur
    end

    def call(file = nil, destination: nil, save: true)
      src = file || @options[:source]
      raise Error, "source file is not provided" unless src

      img =
        if src.is_a?(::Vips::Image)
          src
        elsif src.respond_to?(:path)
          ::Vips::Image.new_from_file(src.path).autorot
        else
          ::Vips::Image.new_from_file(src.to_s).autorot
        end

      (@options[:operations] || []).each do |op, args, kw|
        img = run_op(img, op, args, kw)
      end
      return img unless save

      fmt = @options[:format]
      if fmt.nil? || fmt.empty?
        src_path = src.respond_to?(:path) ? src.path : src.to_s
        ext = File.extname(src_path).delete_prefix(".")
        fmt = ext.empty? ? "png" : ext
      end

      saver_opts = @options[:saver] || {}
      if destination
        img.write_to_target(::Vips::Target.new_to_file(destination.to_s), ".#{fmt}", **saver_opts)
        nil
      else
        tmp = Tempfile.new(["image_processing", ".#{fmt}"])
        tmp.binmode
        img.write_to_target(::Vips::Target.new_to_file(tmp.path), ".#{fmt}", **saver_opts)
        tmp.rewind
        tmp
      end
    end

    def method_missing(name, *args, **kw)
      ops = (@options[:operations] || []) + [[name.to_sym, args, kw]]
      branch(operations: ops)
    end

    private

    def branch(extra)
      self.class.new(@options.merge(extra))
    end

    def run_op(img, op, args, kw)
      case op
      when :resize_to_limit
        w, h = args[0] || ::Vips::MAX_COORD, args[1] || ::Vips::MAX_COORD
        img.thumbnail_image(w, height: h, size: :down, **kw)
      when :resize_to_fit
        w, h = args[0] || ::Vips::MAX_COORD, args[1] || ::Vips::MAX_COORD
        img.thumbnail_image(w, height: h, **kw)
      when :resize_to_fill
        w, h = args[0] || ::Vips::MAX_COORD, args[1] || ::Vips::MAX_COORD
        img.thumbnail_image(w, height: h, crop: kw[:crop] || :centre, **kw)
      when :resize_and_pad
        w, h = args[0], args[1]
        resized = img.thumbnail_image(w, height: h)
        resized = resized.add_alpha if kw[:alpha] && !resized.has_alpha?
        resized.gravity(kw[:gravity] || "centre", w, h, background: kw[:background])
      when :rotate
        deg = args[0].to_f
        case deg
        when 90  then img.rot90
        when 180 then img.rot180
        when 270 then img.rot270
        else img.similarity(angle: deg, **kw)
        end
      else
        if kw.empty?
          img.public_send(op, *args)
        else
          img.public_send(op, *args, **kw)
        end
      end
    end
  end

  module Vips
    def self.valid_image?(file)
      path = file.respond_to?(:path) ? file.path : file.to_s
      ::Vips::Image.new_from_file(path).avg
      true
    rescue ::Vips::Error
      false
    end

    def self.method_missing(name, *args, **kw)
      Builder.new.public_send(name, *args, **kw)
    end
  end
end
