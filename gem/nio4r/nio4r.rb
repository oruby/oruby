module NIO
  class ByteBuffer
    def read_from(io)
      rem = remaining
      raise OverflowError, "buffer is full" if rem == 0
      data = io.read_nonblock(rem, exception: false)
      return 0 if data == :wait_readable
      return 0 if data.nil?
      self << data
      data.bytesize
    end

    def write_to(io)
      rem = remaining
      raise UnderflowError, "no data remaining in buffer" if rem == 0
      saved_pos = position
      data = get(rem)
      written = io.write_nonblock(data, exception: false)
      if written == :wait_writable
        self.position = saved_pos
        return 0
      end
      self.position = saved_pos + written
      written
    end
  end
end
