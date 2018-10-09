package decimal

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
)

// Decimal64 represents an IEEE 754 64-bit floating point decimal number.
// It uses the binary representation method.
type Decimal64 struct {
	bits uint64
}

var neg64 uint64 = 0x8000000000000000
var inf64 uint64 = 0x7800000000000000

// Zero64 represents 0 as a Decimal64.
var Zero64 = newFromParts(0, 0, 0)

// NegZero64 represents -0 as a Decimal64.
var NegZero64 = newFromParts(1, 0, 0)

// One64 represents 1 as a Decimal64.
var One64 = NewDecimal64FromInt64(1)

// NegOne64 represents -1 as a Decimal64.
var NegOne64 = NewDecimal64FromInt64(1).Neg()

// Infinity64 represents ∞ as a Decimal64.
var Infinity64 = Decimal64{inf64}

// NegInfinity64 represents -∞ as a Decimal64.
var NegInfinity64 = Decimal64{neg64 | inf64}

// QNaN64 represents a quiet NaN as a Decimal64.
var QNaN64 = Decimal64{0x7c00000000000000}

// SNaN64 represents a signalling NaN as a Decimal64.
var SNaN64 = Decimal64{0x7e00000000000000}

var zeroes = []Decimal64{Zero64, NegZero64}
var infinities = []Decimal64{Infinity64, NegInfinity64}

const normal = 1000000000000000

type flavor int

const (
	flNormal flavor = iota
	flInf
	flQNaN
	flSNaN
)

func signalNaN64() Decimal64 {
	// TODO: What's the right behavior?
	panic("sNaN64")
}

// NewDecimal64FromInt64 returns a new Decimal64 with the given value.
func NewDecimal64FromInt64(value int64) Decimal64 {
	if value == 0 {
		return Zero64
	}
	sign := 0
	if value < 0 {
		sign = 1
		value = -value
	}
	// TODO: handle abs(value) > 9 999 999 999 999 999
	// lz := bits.LeadingZeros64(uint64(value))
	exp, significand := renormalize(0, uint64(value))
	return newFromParts(sign, exp, significand)
}

func renormalize(exp int, significand uint64) (int, uint64) {
	// TODO: Optimize to O(1) with bits.LeadingZeros64
	for ; significand < 100000000 && exp > -391; exp -= 8 {
		significand *= 100000000
	}
	for ; significand < 1000000000000 && exp > -395; exp -= 4 {
		significand *= 10000
	}
	for ; significand < 100000000000000 && exp > -397; exp -= 2 {
		significand *= 100
	}
	for ; significand < 1000000000000000 && exp > -398; exp-- {
		significand *= 10
	}
	for ; significand > 9999999999999999 && exp < 369; exp++ {
		significand /= 10
	}
	return exp, significand
}

func rescale(exp int, significand uint64, targetExp int) (uint64, int) {
	exp -= targetExp
	var divisor uint64 = 1
	for ; exp < -7 && divisor < significand; exp += 8 {
		divisor *= 100000000
	}
	for ; exp < -3 && divisor < significand; exp += 4 {
		divisor *= 10000
	}
	for ; exp < -1 && divisor < significand; exp += 2 {
		divisor *= 100
	}
	for ; exp < 0 && divisor < significand; exp++ {
		divisor *= 10
	}
	return significand / divisor, targetExp
}

func matchScales(exp1 int, significand1 uint64, exp2 int, significand2 uint64) (int, uint64, int, uint64) {
	if exp1 < exp2 {
		significand1, exp1 = rescale(exp1, significand1, exp2)
	} else if exp2 < exp1 {
		significand2, exp2 = rescale(exp2, significand2, exp1)
	}
	return exp1, significand1, exp2, significand2
}

func newFromParts(sign int, exp int, significand uint64) Decimal64 {
	s := uint64(sign) << 63
	if significand < 0x20000000000000 {
		return Decimal64{s | uint64(exp+398)<<(63-10) | significand}
	}
	significand &= 0x7ffffffffffff
	return Decimal64{s | uint64(exp+398)<<(63-12) | significand | 0x6000000000000000}
}

func (d Decimal64) parts() (fl flavor, sign int, exp int, significand uint64) {
	u := uint64(d.bits)
	sign = int(u >> 63)
	switch (u >> (63 - 4)) & (1<<4 - 1) {
	case 15:
		switch (u >> (63 - 6)) & 3 {
		case 0, 1:
			fl = flInf
		case 2:
			fl = flQNaN
		case 3:
			fl = flSNaN
		}
	case 12, 13, 14:
		// s 11EEeeeeeeee (100)t tttttttttt tttttttttt tttttttttt tttttttttt tttttttttt
		//     EE ∈ {00, 01, 10}
		fl = flNormal
		exp = int((u>>(63-12))&(1<<10-1)) - 398
		significand = u&(1<<51-1) | (1 << 53)
	default:
		// s EEeeeeeeee   (0)ttt tttttttttt tttttttttt tttttttttt tttttttttt tttttttttt
		//   EE ∈ {00, 01, 10}
		fl = flNormal
		exp = int((u>>(63-10))&(1<<10-1)) - 398
		significand = u & (1<<53 - 1)
	}
	return
}

// Abs computes ||d||.
func (d Decimal64) Abs() Decimal64 {
	return Decimal64{^neg64 & uint64(d.bits)}
}

// Add computes d + e
func (d Decimal64) Add(e Decimal64) Decimal64 {
	flavor1, sign1, exp1, significand1 := d.parts()
	flavor2, sign2, exp2, significand2 := e.parts()
	if flavor1 == flSNaN || flavor2 == flSNaN {
		return signalNaN64()
	}
	if flavor1 == flQNaN || flavor2 == flQNaN {
		return QNaN64
	}
	if flavor1 == flInf || flavor2 == flInf {
		if flavor1 != flInf {
			return e
		}
		if flavor2 != flInf || sign1 == sign2 {
			return d
		}
		return QNaN64
	}
	exp1, significand1, exp2, significand2 = matchScales(exp1, significand1, exp2, significand2)
	if sign1 == sign2 {
		significand := significand1 + significand2
		if significand > 9999999999999999 {
			exp1++
			significand /= 10
		}
		return newFromParts(sign1, exp1, significand)
	}
	if significand1 > significand2 {
		return newFromParts(sign1, exp1, significand1-significand2)
	}
	return newFromParts(sign2, exp2, significand2-significand1)
}

// Append is not yet implemented.
func (d Decimal64) Append(buf []byte, fmt byte, prec int) []byte {
	panic("Not implemented")
}

// Cmp returns:
//
//   -1 if d <  e
//    0 if d == e (incl. -0 == 0, -Inf == -Inf, and +Inf == +Inf)
//   +1 if d >  e
//
func (d Decimal64) Cmp(e Decimal64) int {
	flavor1, _, _, _ := d.parts()
	flavor2, _, _, _ := e.parts()
	if flavor1 == flSNaN || flavor2 == flSNaN {
		signalNaN64()
		return 0
	}
	if flavor1 == flQNaN || flavor2 == flQNaN {
		panic("Can't compare NaNs")
	}
	if d == NegZero64 {
		d = Zero64
	}
	if e == NegZero64 {
		e = Zero64
	}
	if d == e {
		return 0
	}
	d = d.Sub(e)
	return 1 - 2*int(d.bits>>63)
}

// Float64 returns a float64 representation of d.
func (d Decimal64) Float64() float64 {
	flavor, sign, exp, significand := d.parts()
	switch flavor {
	case flNormal:
		if significand == 0 {
			return 0.0 * float64(1-2*sign)
		}
		if exp&1 == 1 {
			exp--
			significand *= 10
		}
		return float64(1-2*sign) * float64(significand) * math.Pow10(exp)
	case flInf:
		return math.Inf(1 - 2*sign)
	case flQNaN:
		return math.NaN()
	}
	signalNaN64()
	return 0
}

// Format implements fmt.Formatter.
func (d Decimal64) Format(s fmt.State, format rune) {
	panic("Not implemented")
}

// GobDecode implements encoding.GobDecoder.
func (d *Decimal64) GobDecode(buf []byte) error {
	d.bits = binary.BigEndian.Uint64(buf)
	// TODO: Check for out of bounds significand.
	return nil
}

// GobEncode implements encoding.GobEncoder.
func (d Decimal64) GobEncode() ([]byte, error) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, d.bits)
	return buf, nil
}

// Int64 converts d to an int64.
func (d Decimal64) Int64() int64 {
	flavor, sign, exp, significand := d.parts()
	if flavor != flNormal {
		panic("Not implemented")
	}
	for ; exp > 0; exp-- {
		significand *= 10
	}
	if exp < 0 {
		var divisor uint64 = 1
		for ; exp < 0; exp++ {
			divisor *= 10
		}
		significand /= divisor
	}
	if sign == 0 {
		return int64(significand)
	}
	return -int64(significand)
}

// IsInf returns true iff d = ±∞.
func (d Decimal64) IsInf() bool {
	flavor, _, _, _ := d.parts()
	return flavor == flInf
}

// IsNaN returns true iff d is not a number.
func (d Decimal64) IsNaN() bool {
	flavor, _, _, _ := d.parts()
	return flavor == flQNaN || flavor == flSNaN
}

// IsQNaN returns true iff d is a quiet NaN.
func (d Decimal64) IsQNaN() bool {
	flavor, _, _, _ := d.parts()
	return flavor == flQNaN
}

// IsSNaN returns true iff d is a signalling NaN.
func (d Decimal64) IsSNaN() bool {
	flavor, _, _, _ := d.parts()
	return flavor == flQNaN
}

// IsInt is not yet implemented.
func (d Decimal64) IsInt() bool {
	panic("Not implemented")
}

// MarshalText implements the encoding.TextMarshaler interface.
func (d Decimal64) MarshalText() []byte {
	var buf []byte
	return d.Append(buf, 'g', -1)
}

// Mul computes d * e.
func (d Decimal64) Mul(e Decimal64) Decimal64 {
	flavor1, sign1, exp1, significand1 := d.parts()
	flavor2, sign2, exp2, significand2 := e.parts()

	if flavor1 == flSNaN || flavor2 == flSNaN {
		return signalNaN64()
	}
	if flavor1 == flQNaN || flavor2 == flQNaN {
		return QNaN64
	}

	sign := sign1 ^ sign2
	if d == Zero64 || d == NegZero64 || e == Zero64 || e == NegZero64 {
		return zeroes[sign]
	}
	if flavor1 == flInf || flavor2 == flInf {
		return infinities[sign]
	}

	exp := exp1 + exp2
	significand := umul64(significand1, significand2)
	for significand.hi > 0 || significand.lo > 9999999999999999 {
		exp++
		significand = significand.divBy10()
	}

	return newFromParts(sign, exp, significand.lo)
}

// Neg computes -d.
func (d Decimal64) Neg() Decimal64 {
	return Decimal64{neg64 ^ uint64(d.bits)}
}

// ParseDecimal64 is not yet implemented.
func ParseDecimal64(s string, base int) (Decimal64, error) {
	panic("Not implemented")
}

// Quo computes d / e.
func (d Decimal64) Quo(e Decimal64) Decimal64 {
	flavor1, sign1, exp1, significand1 := d.parts()
	flavor2, sign2, exp2, significand2 := e.parts()

	if flavor1 == flSNaN || flavor2 == flSNaN {
		return signalNaN64()
	}
	if flavor1 == flQNaN || flavor2 == flQNaN {
		return QNaN64
	}

	sign := sign1 ^ sign2
	if d == Zero64 || d == NegZero64 {
		if e == Zero64 || e == NegZero64 {
			return QNaN64
		}
		return zeroes[sign]
	}
	if flavor1 == flInf {
		if flavor2 == flInf {
			return QNaN64
		}
		return infinities[sign]
	}
	if flavor2 == flInf {
		return zeroes[sign]
	}

	exp := exp1 - exp2 - 16
	significand := umul64(10000000000000000, significand1).div64(significand2)
	for significand.hi > 0 || significand.lo > 9999999999999999 {
		exp++
		significand = significand.divBy10()
	}

	return newFromParts(sign, exp, significand.lo)
}

// Scan is not yet implemented.
func (d Decimal64) Scan(s fmt.ScanState, ch rune) error {
	panic("Not implemented")
}

// Sign is not yet implemented.
func (d Decimal64) Sign() int {
	panic("Not implemented")
}

// Signbit is not yet implemented.
func (d Decimal64) Signbit() bool {
	panic("Not implemented")
}

// Sqrt computes √d.
func (d Decimal64) Sqrt() Decimal64 {
	flavor, sign, exp, significand := d.parts()
	switch flavor {
	case flNormal:
		if significand == 0 {
			return d
		}
		if sign == 1 {
			return QNaN64
		}
		if exp&1 == 1 {
			exp--
			significand *= 10
		}
		sqrt := umul64(10000000000000000, significand).sqrt()
		exp, significand = renormalize(exp/2-8, sqrt)
		return newFromParts(sign, exp, significand)
	case flInf:
		return d
	case flQNaN:
		return d
	case flSNaN:
		return signalNaN64()
	}
	return Decimal64{}
}

// String returns a string representation of d.
func (d Decimal64) String() string {
	// TODO: Implement non-integers
	flavor, sign, _, _ := d.parts()
	switch flavor {
	case flNormal:
		if d == NegZero64 {
			return "-0"
		}
		return strconv.FormatInt(d.Int64(), 10)
	case flInf:
		if sign == 0 {
			return "∞"
		}
		return "-∞"
	case flQNaN, flSNaN:
		return "NaN"
	}
	return ""
}

// Sub returns d - e.
func (d Decimal64) Sub(e Decimal64) Decimal64 {
	return d.Add(e.Neg())
}

// Text converts the floating-point number x to a string according to the given
// format and precision prec.
func (d Decimal64) Text(format byte, prec int) string {
	panic("Not implemented")
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (d *Decimal64) UnmarshalText(text []byte) error {
	var err error
	*d, err = ParseDecimal64(string(text), 0)
	if err != nil {
		err = fmt.Errorf("decimal: cannot unmarshal %q as Decimal64 (%v)", text, err)
	}
	return err
}