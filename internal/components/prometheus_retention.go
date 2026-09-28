package components

import (
	"errors"
	"fmt"
	"math"
)

// parsePrometheusRetentionSize targets the size syntax of Prometheus v3.14.0:
// https://github.com/prometheus/prometheus/blob/v3.14.0/config/config.go (TSDBRetentionConfig).
// Recheck compatibility when updating the locked Prometheus version; local syntax
// examples do not establish complete parity with upstream numeric conversions.
func parsePrometheusRetentionSize(value string) (float64, error) {
	size, err := parsePrometheusRetentionSizeWithUnitStyle(value, true)
	if err == nil {
		return size, nil
	}

	return parsePrometheusRetentionSizeWithUnitStyle(value, false)
}

func parsePrometheusRetentionSizeWithUnitStyle(
	value string,
	useIECUnits bool,
) (float64, error) {
	originalValue := value
	negative := false
	if value != "" && (value[0] == '-' || value[0] == '+') {
		negative = value[0] == '-'
		value = value[1:]
	}
	if value == "0" {
		return 0, nil
	}
	if value == "" {
		return 0, fmt.Errorf("invalid retention size %q", originalValue)
	}

	size := float64(0)
	for value != "" {
		amount, amountLength, err := consumeRetentionSizeAmount(value)
		if err != nil {
			return 0, fmt.Errorf("invalid retention size %q: %w", originalValue, err)
		}
		value = value[amountLength:]

		unitEnd := 0
		for unitEnd < len(value) && value[unitEnd] != '.' && !isASCIIDigit(value[unitEnd]) {
			unitEnd++
		}
		unit := value[:unitEnd]
		multiplier, ok := retentionSizeMultiplier(unit, useIECUnits)
		if !ok {
			return 0, fmt.Errorf("invalid retention size %q: unknown unit %q", originalValue, unit)
		}

		size += amount * multiplier
		value = value[unitEnd:]
	}

	if negative {
		size = -size
	}
	if size < float64(math.MinInt64) || size > float64(math.MaxInt64) {
		return 0, fmt.Errorf("retention size %q overflows int64", originalValue)
	}

	return size, nil
}

func consumeRetentionSizeAmount(value string) (float64, int, error) {
	integer, integerDigits, err := consumeRetentionSizeDigits(value)
	if err != nil {
		return 0, 0, err
	}
	amount := float64(integer)
	amountLength := integerDigits
	hasDigits := integerDigits > 0

	if amountLength < len(value) && value[amountLength] == '.' {
		amountLength++
		fraction, fractionDigits, err := consumeRetentionSizeDigits(value[amountLength:])
		if err != nil {
			return 0, 0, err
		}
		amountLength += fractionDigits
		hasDigits = hasDigits || fractionDigits > 0

		scale := float64(1)
		for range fractionDigits {
			scale *= 10
		}
		amount += float64(fraction) / scale
	}
	if !hasDigits {
		return 0, 0, errors.New("expected a number")
	}

	return amount, amountLength, nil
}

func consumeRetentionSizeDigits(value string) (int64, int, error) {
	number := int64(0)
	digits := 0
	for digits < len(value) && isASCIIDigit(value[digits]) {
		if number >= (1<<63-10)/10 {
			return 0, 0, errors.New("number overflows int64")
		}
		number = number*10 + int64(value[digits]-'0')
		digits++
	}

	return number, digits, nil
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func retentionSizeMultiplier(unit string, useIECUnits bool) (float64, bool) {
	if unit == "B" {
		return 1, true
	}
	if useIECUnits {
		switch unit {
		case "KiB":
			return 1 << 10, true
		case "MiB":
			return 1 << 20, true
		case "GiB":
			return 1 << 30, true
		case "TiB":
			return 1 << 40, true
		case "PiB":
			return 1 << 50, true
		case "EiB":
			return 1 << 60, true
		}
	} else {
		switch unit {
		case "KB":
			return 1 << 10, true
		case "MB":
			return 1 << 20, true
		case "GB":
			return 1 << 30, true
		case "TB":
			return 1 << 40, true
		case "PB":
			return 1 << 50, true
		case "EB":
			return 1 << 60, true
		}
	}

	return 0, false
}
