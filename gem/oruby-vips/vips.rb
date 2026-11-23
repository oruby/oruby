module Vips
  MAX_COORD = 10_000_000

  def self.oruby_backend
    :go
  end

  def self.version_string
    "go-8.15.0"
  end

  def self.at_least_libvips?(major, minor = 0)
    major < 8 || (major == 8 && minor <= 15)
  end

  def self.block_untrusted(_state = true)
    nil
  end

  def self.p2str(val)
    val.nil? ? "" : val.to_s
  end

  class Target
    attr_reader :filename

    def self.new_to_file(filename)
      new(filename)
    end

    def initialize(filename)
      @filename = filename.to_s
    end
  end

  class Introspect
    OPT_SET = {
      "Q" => true, "compression" => true, "strip" => true, "interlace" => true,
      "optimize_coding" => true, "subsample_mode" => true, "palette" => true,
      "access" => true, "page" => true, "n" => true, "autorotate" => true,
      "fail_on" => true, "background" => true, "precision" => true,
    }.freeze

    def self.get(_operation_name)
      new
    end

    def optional_input
      OPT_SET
    end
  end

  class Image
    def self.new_from_file(path, **_opts)
      __new_from_file(path.to_s)
    end

    def self.new_from_buffer(data, option_string = "", **_opts)
      __new_from_buffer(data, option_string.to_s)
    end

    def self.black(width, height, bands: 1, **_opts)
      __black(width.to_i, height.to_i, bands.to_i)
    end

    def self.new_from_array(matrix, scale = 1.0, offset = 0.0)
      __new_from_array(matrix, scale.to_f, offset.to_f)
    end

    def self.new_from_memory(data, width, height, bands, _format = :uchar)
      __new_from_memory(data, width.to_i, height.to_i, bands.to_i)
    end

    def self.thumbnail(filename, width, height: nil, size: :both, crop: :none, no_rotate: false, auto_rotate: true, **_opts)
      img = __new_from_file(filename.to_s)
      img = img.autorot if auto_rotate && !no_rotate
      img.thumbnail_image(width, height: height, size: size, crop: crop)
    end

    def self.thumbnail_buffer(data, width, height: nil, size: :both, crop: :none, no_rotate: false, auto_rotate: true, **_opts)
      img = __new_from_buffer(data, "")
      img = img.autorot if auto_rotate && !no_rotate
      img.thumbnail_image(width, height: height, size: size, crop: crop)
    end

    def size
      [width, height]
    end

    def thumbnail_image(width, height: nil, size: :both, crop: :none, **_opts)
      __thumbnail_image(width.to_i, height ? height.to_i : 0, size.to_s, crop.to_s)
    end

    def resize(scale, vscale: nil, kernel: :lanczos3, **_opts)
      vs = vscale ? vscale.to_f : scale.to_f
      __resize(scale.to_f, vs, kernel.to_s)
    end

    def crop(left, top, w, h)
      __crop(left.to_i, top.to_i, w.to_i, h.to_i)
    end
    alias extract_area crop

    def rot(angle)
      case angle.to_s
      when "d90", "90"   then rot90
      when "d180", "180" then rot180
      when "d270", "270" then rot270
      else self
      end
    end

    def similarity(angle: 0.0, scale: 1.0, background: nil, **_opts)
      img = scale.to_f == 1.0 ? self : resize(scale.to_f)
      img.__rotate_angle(angle.to_f, background)
    end

    def flip(direction)
      __flip(direction.to_s)
    end

    def gravity(direction, width, height, extend: nil, background: nil, **_opts)
      __gravity(direction.to_s, width.to_i, height.to_i, background)
    end

    def composite(overlays, mode = :over, **_opts)
      list = overlays.is_a?(Array) ? overlays : [overlays]
      cur = self
      list.each do |ov|
        cur = cur.__composite_over(ov)
      end
      cur
    end
    alias composite2 composite

    def colourspace(space, **_opts)
      __colourspace(space.to_s)
    end

    def gaussblur(sigma, **_opts)
      __gaussblur(sigma.to_f)
    end

    def conv(mask, **_opts)
      __conv(mask)
    end

    def flatten(background: nil, **_opts)
      __flatten(background)
    end

    def linear(a, b, **_opts)
      av = a.is_a?(Array) ? a.first.to_f : a.to_f
      bv = b.is_a?(Array) ? b.first.to_f : b.to_f
      __linear(av, bv)
    end

    def extract_band(band, **_opts)
      __extract_band(band.to_i)
    end

    def write_to_file(path, **opts)
      q = (opts[:Q] || opts[:quality] || 85).to_i
      __write_to_file(path.to_s, q)
    end

    def write_to_buffer(format_string, **opts)
      q = (opts[:Q] || opts[:quality] || 85).to_i
      __write_to_buffer(format_string.to_s, q)
    end

    def write_to_target(target, format_string, **opts)
      q = (opts[:Q] || opts[:quality] || 85).to_i
      __write_to_target(target.filename, format_string.to_s, q)
    end

    def pngsave(path, **opts);  write_to_file(path, **opts); end
    def jpegsave(path, **opts); write_to_file(path, **opts); end
    def gifsave(path, **opts);  write_to_file(path, **opts); end
    def bmpsave(path, **opts);  write_to_file(path, **opts); end
    def tiffsave(path, **opts); write_to_file(path, **opts); end
    def webpsave(_path, **_opts)
      raise Vips::Error, "unsupported output format .webp in pure-Go Vips backend (install libvips for WebP encoding)"
    end
    def heifsave(_path, **_opts)
      raise Vips::Error, "unsupported output format .heif/.avif in pure-Go Vips backend (install libvips for AVIF/HEIF encoding)"
    end

    def set_type(_gtype, name, value)
      set(name, value)
    end
    alias set_value set
  end
end
