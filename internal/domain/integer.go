package domain

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// Integer preserves exact JSON Schema integer values, including integral decimal
// and exponent forms. Schema validation still owns field-specific bounds.
type Integer int64

// UnmarshalJSON decodes without float rounding or exponent-sized allocations.
// Failed decoding leaves the receiver unchanged; marshaling uses an integer token.
func (value *Integer) UnmarshalJSON(data []byte) error {
	decoded, err := parseInteger(data)
	if err != nil {
		return err
	}
	*value = Integer(decoded)
	return nil
}

var errInvalidInteger = errors.New("expected an exact signed 64-bit JSON integer")

func parseInteger(data []byte) (int64, error) {
	if !json.Valid(data) {
		return 0, errInvalidInteger
	}
	literal := strings.TrimSpace(string(data))
	isNegative := strings.HasPrefix(literal, "-")
	literal = strings.TrimPrefix(literal, "-")
	if literal[0] < '0' || literal[0] > '9' {
		return 0, errInvalidInteger
	}
	mantissa, exponentText := literal, "0"
	if position := strings.IndexAny(literal, "eE"); position >= 0 {
		mantissa, exponentText = literal[:position], literal[position+1:]
	}
	fractionDigits := 0
	if position := strings.IndexByte(mantissa, '.'); position >= 0 {
		fractionDigits = len(mantissa) - position - 1
	}
	digits := strings.TrimLeft(strings.ReplaceAll(mantissa, ".", ""), "0")
	if digits == "" {
		return 0, nil
	}
	exponent, err := strconv.ParseInt(exponentText, 10, 64)
	if err != nil || exponent > int64(len(data))+19 || exponent < -int64(len(data)) {
		return 0, errInvalidInteger
	}
	shift := exponent - int64(fractionDigits)
	if shift < 0 {
		places := -shift
		if places >= int64(len(digits)) {
			return 0, errInvalidInteger
		}
		cut := len(digits) - int(places)
		if strings.Trim(digits[cut:], "0") != "" {
			return 0, errInvalidInteger
		}
		digits, shift = digits[:cut], 0
	}
	if len(digits) > 19 || shift > int64(19-len(digits)) {
		return 0, errInvalidInteger
	}
	digits += strings.Repeat("0", int(shift))
	if isNegative {
		digits = "-" + digits
	}
	decoded, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, errInvalidInteger
	}
	return decoded, nil
}
