module RbConfig
  SIZEOF = {
    "int" => 4, "short" => 2, "long" => 8, "long long" => 8, "__int128" => 16,
    "off_t" => 8, "void*" => 8, "float" => 4, "double" => 8, "time_t" => 8,
    "clock_t" => 8, "size_t" => 8, "ptrdiff_t" => 8, "dev_t" => 4, "int8_t" => 1,
    "uint8_t" => 1, "int16_t" => 2, "uint16_t" => 2, "int32_t" => 4, "uint32_t" => 4,
    "int64_t" => 8, "uint64_t" => 8, "intptr_t" => 8, "uintptr_t" => 8, "ssize_t" => 8,
    "int_least8_t" => 1, "int_least16_t" => 2, "int_least32_t" => 4, "int_least64_t" => 8,
    "int_fast8_t" => 1, "int_fast16_t" => 2, "int_fast32_t" => 4, "int_fast64_t" => 8,
    "intmax_t" => 8, "sig_atomic_t" => 4, "wchar_t" => 4, "wint_t" => 4, "wctrans_t" => 8,
    "wctype_t" => 4, "_Bool" => 1, "long double" => 16, "float _Complex" => 8,
    "double _Complex" => 16, "long double _Complex" => 32, "__float128" => 16,
  }
  LIMITS = {
    "FIXNUM_MAX" => 0x7fffffffffffffff, "FIXNUM_MIN" => -0x8000000000000000,
    "CHAR_MAX" => 127, "CHAR_MIN" => -128, "SCHAR_MAX" => 127, "SCHAR_MIN" => -128,
    "UCHAR_MAX" => 255, "SHRT_MAX" => 32767, "SHRT_MIN" => -32768, "USHRT_MAX" => 65535,
    "INT_MAX" => 2147483647, "INT_MIN" => -2147483648, "UINT_MAX" => 4294967295,
    "LONG_MAX" => 0x7fffffffffffffff, "LONG_MIN" => -0x8000000000000000,
    "LLONG_MAX" => 0x7fffffffffffffff, "LLONG_MIN" => -0x8000000000000000,
  }
end
