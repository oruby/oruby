package complex

import (
	"errors"
	"fmt"
	"github.com/oruby/oruby"
	"math"
	"math/cmplx"
)

// RComplex embeds Go complex128 value
type RComplex struct {
	v complex128
}

func newComplex(r, i float64) *RComplex {
	return &RComplex{complex(r, i)}
}

// Real returns real part of complex number
func (c *RComplex) Real() float64 {
	return real(c.v)
}

// Imaginary returns imaginary part of complex number
func (c *RComplex) Imaginary() float64 {
	return imag(c.v)
}

// ToF converts complex number to float
// it returns ERageError if imaginary part is not zero
func (c *RComplex) ToF() (float64, error) {
	if imag(c.v) != 0 {
		return 0, oruby.ERangeError("can't convert %v into Float", c.v)
	}
	return real(c.v), nil
}

// ToI converts complex number to int
// it returns ERageError if imaginary part is not zero
func (c *RComplex) ToI() (int, error) {
	if imag(c.v) != 0 {
		return 0, oruby.ERangeError("can't convert %v into int", c.v)
	}
	return int(real(c.v)), nil
}

// ToC creates new instance of complex number
func (c *RComplex) ToC() *RComplex {
	return &RComplex{c.v}
}

// DivideWith implements complex division
func (c *RComplex) DivideWith(c2 *RComplex) *RComplex {
	return &RComplex{c.v / c2.v}
}

// Polar instance of complex number
func Polar(abs, arg float64) *RComplex {
	return &RComplex{complex(abs*math.Cos(arg), math.Sin(arg))}
}

// Inspect returns string representation of complex number
func (c *RComplex) Inspect() string {
	return fmt.Sprintf("%v", c.v)
}

// String implements Stringer interface for RComplex
func (c *RComplex) String() string {
	return fmt.Sprintf("%v", c.v)
}

// ToS printscomplex number without parenthesis
func (c *RComplex) ToS() string {
	s := fmt.Sprintf("%v", c.v)
	return s[1 : len(s)-1]
}

func toInt64(in interface{}) (int64, error) {
	switch v := in.(type) {
	case nil:
		return 0, nil
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
	default:
		return 0, errors.New("not a number")
	}
}

func toFloat64(in interface{}) (float64, error) {
	switch v := in.(type) {
	case nil:
		return 0, nil
	case int:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	default:
		return 0, errors.New("not a floatable number")
	}
}

func toComplex(in interface{}) (complex128, error) {
	if v, ok := in.(*RComplex); ok {
		return v.v, nil
	}

	v, err := toFloat64(in)
	if err != nil {
		return 0, err
	}
	return complex(v, 0), nil
}

// UnaryPlusOperator implements unary plus operator for complex number
func (c *RComplex) UnaryPlusOperator() *RComplex {
	return &RComplex{c.v}
}

// UnaryMinusOperator implements unary minus operator for complex number
func (c *RComplex) UnaryMinusOperator() *RComplex {
	return &RComplex{-c.v}
}

// PlusOperator implements complex number addition
func (c *RComplex) PlusOperator(v2 interface{}) (*RComplex, error) {
	v, err := toComplex(v2)
	if err != nil {
		return nil, err
	}
	return &RComplex{c.v + v}, nil
}

// MinusOperator implements complex number substractions
func (c *RComplex) MinusOperator(v2 interface{}) (*RComplex, error) {
	v, err := toComplex(v2)
	if err != nil {
		return nil, err
	}
	return &RComplex{c.v - v}, nil
}

// MultiplyOperator implements complex number multiplication
func (c *RComplex) MultiplyOperator(v2 interface{}) (*RComplex, error) {
	v, err := toComplex(v2)
	if err != nil {
		return nil, err
	}
	return &RComplex{c.v * v}, nil
}

// DivideOperator implements complex number division
func (c *RComplex) DivideOperator(v2 interface{}) (*RComplex, error) {
	v, err := toComplex(v2)
	if err != nil {
		return nil, err
	}
	return &RComplex{c.v / v}, nil
}

// EqualOperator cheks equalitu to other numbers
func (c *RComplex) EqualOperator(v2 interface{}) bool {
	v, err := toComplex(v2)
	if err != nil {
		return false
	}
	return c.v == v
}

// Abs or Magnitude of complex number
func (c *RComplex) Abs() float64 {
	return cmplx.Abs(c.v)
}

// Abs2 of complex number
func (c *RComplex) Abs2() float64 {
	return real(c.v)*real(c.v) + imag(c.v)*imag(c.v)
}

// Arg or Phase or Angle of complex number
func (c *RComplex) Arg() float64 {
	return cmplx.Phase(c.v)
}

// Conjugate or Conj of complex number
func (c *RComplex) Conjugate() *RComplex {
	return &RComplex{cmplx.Conj(c.v)}
}

// Fdiv implements division with floating point number
func (c *RComplex) Fdiv(num float64) *RComplex {
	return &RComplex{complex(real(c.v)/num, imag(c.v)/num)}
}

// Polar of complex number
func (c *RComplex) Polar() (float64, float64) {
	return cmplx.Polar(c.v)
}

// IsReal returns false for complex number objects
func (c *RComplex) IsReal() bool { return false }

// Rectangular values of real and imaginary of complex number
func (c *RComplex) Rectangular() (float64, float64) {
	return real(c.v), imag(c.v)
}

// ToR conver to Rational
// TODO: Rational numbers
//func (c *RComplex) ToR (*Rational, error) {
// if imag(c.v) != 0 {
//    return nil, fmt.Errorf("can't convert %v into Rational", c.v)
// }
// return &Rational(real, 1)
//}
