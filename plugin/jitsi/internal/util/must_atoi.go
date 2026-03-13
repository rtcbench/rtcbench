package util

import (
	"fmt"
	"strconv"
)

// MustAtoi converts string to int, panics if conversion fails.
func MustAtoi(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		panic(fmt.Sprintf("MustAtoi: cannot convert %q to int: %v", s, err))
	}
	return i
}

// MustAtoi64 converts string to int64, panics if conversion fails.
func MustAtoi64(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("MustAtoi64: cannot convert %q to int64: %v", s, err))
	}
	return i
}
