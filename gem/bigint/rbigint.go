package bigint

import (
	"fmt"
	"math/big"
	"strings"
)

// RBigInt wraps Go *big.Int for oruby
type RBigInt struct {
	i *big.Int
}

func newBigInt() *RBigInt {
	return &RBigInt{i: new(big.Int)}
}

func newBigIntFromInt64(v int64) *RBigInt {
	return &RBigInt{i: big.NewInt(v)}
}

func newBigIntFromBig(bi *big.Int) *RBigInt {
	if bi == nil {
		bi = new(big.Int)
	}
	return &RBigInt{i: new(big.Int).Set(bi)}
}

// New returns a new RBigInt wrapping the given *big.Int.
func New(bi *big.Int) *RBigInt {
	return newBigIntFromBig(bi)
}

func toBigInt(in interface{}) (*big.Int, error) {
	switch v := in.(type) {
	case nil:
		return big.NewInt(0), nil
	case *RBigInt:
		if v == nil || v.i == nil {
			return big.NewInt(0), nil
		}
		return v.i, nil
	case RBigInt:
		if v.i == nil {
			return big.NewInt(0), nil
		}
		return v.i, nil
	case int:
		return big.NewInt(int64(v)), nil
	case int64:
		return big.NewInt(v), nil
	case int32:
		return big.NewInt(int64(v)), nil
	case int16:
		return big.NewInt(int64(v)), nil
	case int8:
		return big.NewInt(int64(v)), nil
	case uint:
		return new(big.Int).SetUint64(uint64(v)), nil
	case uint64:
		return new(big.Int).SetUint64(v), nil
	case uint32:
		return new(big.Int).SetUint64(uint64(v)), nil
	case uint16:
		return new(big.Int).SetUint64(uint64(v)), nil
	case uint8:
		return new(big.Int).SetUint64(uint64(v)), nil
	case float64:
		bi, _ := big.NewFloat(v).Int(nil)
		if bi == nil {
			bi = big.NewInt(0)
		}
		return bi, nil
	case float32:
		bi, _ := big.NewFloat(float64(v)).Int(nil)
		if bi == nil {
			bi = big.NewInt(0)
		}
		return bi, nil
	case string:
		s := strings.TrimSpace(v)
		base := 10
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			base = 16
			s = s[2:]
		} else if strings.HasPrefix(s, "0b") || strings.HasPrefix(s, "0B") {
			base = 2
			s = s[2:]
		} else if strings.HasPrefix(s, "0o") || strings.HasPrefix(s, "0O") {
			base = 8
			s = s[2:]
		}
		bi, ok := new(big.Int).SetString(s, base)
		if !ok {
			return nil, fmt.Errorf("invalid string for Bignum: %q", v)
		}
		return bi, nil
	case *big.Int:
		if v == nil {
			return big.NewInt(0), nil
		}
		return v, nil
	default:
		return nil, fmt.Errorf("cannot convert %T into Bignum", in)
	}
}

func (b *RBigInt) Value() *big.Int {
	if b == nil || b.i == nil {
		return big.NewInt(0)
	}
	return b.i
}

func (b *RBigInt) PlusOperator(v2 interface{}) (*RBigInt, error) {
	bi2, err := toBigInt(v2)
	if err != nil {
		return nil, err
	}
	res := new(big.Int).Add(b.Value(), bi2)
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) MinusOperator(v2 interface{}) (*RBigInt, error) {
	bi2, err := toBigInt(v2)
	if err != nil {
		return nil, err
	}
	res := new(big.Int).Sub(b.Value(), bi2)
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) MultiplyOperator(v2 interface{}) (*RBigInt, error) {
	bi2, err := toBigInt(v2)
	if err != nil {
		return nil, err
	}
	res := new(big.Int).Mul(b.Value(), bi2)
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) DivideOperator(v2 interface{}) (*RBigInt, error) {
	bi2, err := toBigInt(v2)
	if err != nil {
		return nil, err
	}
	if bi2.Sign() == 0 {
		return nil, fmt.Errorf("divided by 0")
	}
	res := new(big.Int).Quo(b.Value(), bi2)
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) ModuloOperator(v2 interface{}) (*RBigInt, error) {
	bi2, err := toBigInt(v2)
	if err != nil {
		return nil, err
	}
	if bi2.Sign() == 0 {
		return nil, fmt.Errorf("divided by 0")
	}
	res := new(big.Int).Mod(b.Value(), bi2)
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) PowerOperator(exp int64) (*RBigInt, error) {
	if exp < 0 {
		return nil, fmt.Errorf("negative exponent")
	}
	res := new(big.Int).Exp(b.Value(), big.NewInt(exp), nil)
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) UnaryMinusOperator() *RBigInt {
	res := new(big.Int).Neg(b.Value())
	return newBigIntFromBig(res)
}

func (b *RBigInt) UnaryPlusOperator() *RBigInt {
	return newBigIntFromBig(b.Value())
}

func (b *RBigInt) InvertOperator() *RBigInt {
	// ~x = -(x + 1)
	res := new(big.Int).Not(b.Value())
	return newBigIntFromBig(res)
}

func (b *RBigInt) BitAndOperator(v2 interface{}) (*RBigInt, error) {
	bi2, err := toBigInt(v2)
	if err != nil {
		return nil, err
	}
	res := new(big.Int).And(b.Value(), bi2)
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) BitOrOperator(v2 interface{}) (*RBigInt, error) {
	bi2, err := toBigInt(v2)
	if err != nil {
		return nil, err
	}
	res := new(big.Int).Or(b.Value(), bi2)
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) BitXorOperator(v2 interface{}) (*RBigInt, error) {
	bi2, err := toBigInt(v2)
	if err != nil {
		return nil, err
	}
	res := new(big.Int).Xor(b.Value(), bi2)
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) LeftShiftOperator(n int64) (*RBigInt, error) {
	if n < 0 {
		return b.RightShiftOperator(-n)
	}
	res := new(big.Int).Lsh(b.Value(), uint(n))
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) RightShiftOperator(n int64) (*RBigInt, error) {
	if n < 0 {
		return b.LeftShiftOperator(-n)
	}
	res := new(big.Int).Rsh(b.Value(), uint(n))
	return newBigIntFromBig(res), nil
}

func (b *RBigInt) Bit(n int) int {
	if n < 0 {
		return 0
	}
	return int(b.Value().Bit(n))
}

func (b *RBigInt) Cmp(v2 interface{}) (int, error) {
	bi2, err := toBigInt(v2)
	if err != nil {
		return 0, err
	}
	return b.Value().Cmp(bi2), nil
}

func (b *RBigInt) EqualOperator(v2 interface{}) bool {
	bi2, err := toBigInt(v2)
	if err != nil {
		return false
	}
	return b.Value().Cmp(bi2) == 0
}

func (b *RBigInt) LessOperator(v2 interface{}) bool {
	bi2, err := toBigInt(v2)
	if err != nil {
		return false
	}
	return b.Value().Cmp(bi2) < 0
}

func (b *RBigInt) LessEqualOperator(v2 interface{}) bool {
	bi2, err := toBigInt(v2)
	if err != nil {
		return false
	}
	return b.Value().Cmp(bi2) <= 0
}

func (b *RBigInt) GreaterOperator(v2 interface{}) bool {
	bi2, err := toBigInt(v2)
	if err != nil {
		return false
	}
	return b.Value().Cmp(bi2) > 0
}

func (b *RBigInt) GreaterEqualOperator(v2 interface{}) bool {
	bi2, err := toBigInt(v2)
	if err != nil {
		return false
	}
	return b.Value().Cmp(bi2) >= 0
}

func (b *RBigInt) Abs() *RBigInt {
	res := new(big.Int).Abs(b.Value())
	return newBigIntFromBig(res)
}

func (b *RBigInt) ToI() int64 {
	return b.Value().Int64()
}

func (b *RBigInt) ToF() float64 {
	f, _ := new(big.Float).SetInt(b.Value()).Float64()
	return f
}

func (b *RBigInt) ToS(base ...int) string {
	radix := 10
	if len(base) > 0 && base[0] >= 2 && base[0] <= 36 {
		radix = base[0]
	}
	return b.Value().Text(radix)
}

func (b *RBigInt) Inspect() string {
	return b.Value().String()
}

func (b *RBigInt) String() string {
	return b.Value().String()
}

func (b *RBigInt) IsEven() bool {
	return b.Value().Bit(0) == 0
}

func (b *RBigInt) IsOdd() bool {
	return b.Value().Bit(0) == 1
}

func (b *RBigInt) IsZero() bool {
	return b.Value().Sign() == 0
}

func (b *RBigInt) BitLength() int {
	return b.Value().BitLen()
}

func (b *RBigInt) Size() int {
	return (b.Value().BitLen() + 7) / 8
}
