package domain

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestIntegerDecodingPreservesExactValues(t *testing.T) {
	cases := map[string]int64{
		"60.0":                      60,
		"6e1":                       60,
		"0.00060e5":                 60,
		"600e-1":                    60,
		"-0e99999999999999999999":   0,
		"0.0e-99999999999999999999": 0,
		"9007199254740991.0":        9007199254740991,
		"9007199254740991e0":        9007199254740991,
		"9223372036854775807.0":     9223372036854775807,
		"-92233720368547758080e-1":  -9223372036854775808,
	}
	for input, expected := range cases {
		t.Run(input, func(t *testing.T) {
			var value Integer
			if err := json.Unmarshal([]byte(input), &value); err != nil || int64(value) != expected {
				t.Fatalf("expected %d, received %d: %v", expected, value, err)
			}
			encoded, err := json.Marshal(value)
			if err != nil || string(encoded) != strconv.FormatInt(expected, 10) {
				t.Fatalf("integer marshal changed value: %s: %v", encoded, err)
			}
		})
	}
}

func TestIntegerRejectsFractionsOverflowAndOtherJsonTypes(t *testing.T) {
	for _, input := range []string{"60.1", "6e-1", "9007199254740990.0000001", "9223372036854775808", "-9223372036854775809", "1e99999999999999999999", "1e-99999999999999999999", `"60"`, "null", "true", "{}", "[]", "", "01", "+1", "1_000"} {
		t.Run(input, func(t *testing.T) {
			value := Integer(7)
			if err := json.Unmarshal([]byte(input), &value); err == nil {
				t.Fatal("invalid integer accepted")
			}
			if value != 7 {
				t.Fatal("failed decode mutated destination")
			}
		})
	}
}
