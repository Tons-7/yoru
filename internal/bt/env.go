package bt

import (
	"os"
	"strconv"

	"golang.org/x/exp/constraints"
)

func initIntFromEnv[T constraints.Signed](key string, defaultValue T, bitSize int) T {
	return strconvFromEnv(key, defaultValue, bitSize, strconv.ParseInt)
}

func initUIntFromEnv[T constraints.Unsigned](key string, defaultValue T, bitSize int) T {
	return strconvFromEnv(key, defaultValue, bitSize, strconv.ParseUint)
}

func strconvFromEnv[T, U constraints.Integer](key string, defaultValue T, bitSize int, conv func(s string, base, bitSize int) (U, error)) T {
	s := os.Getenv(key)
	if s == "" {
		return defaultValue
	}
	i64, err := conv(s, 10, bitSize)
	if err != nil || i64 <= 0 {
		return defaultValue
	}
	return T(i64)
}
