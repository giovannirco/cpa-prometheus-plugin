package main

import (
	"errors"
	"math"
)

const (
	maxHostRequestBytes  = 1 << 20
	maxHostMethodBytes   = 256
	maxHostResponseBytes = 1 << 20
	pluginABIVersion     = 1
)

var errHostBufferTooLarge = errors.New("host buffer exceeds plugin limit")

func boundedLen(n uint64, max int) (int, error) {
	if max <= 0 {
		return 0, errHostBufferTooLarge
	}
	if n > uint64(max) || n > uint64(math.MaxInt32) {
		return 0, errHostBufferTooLarge
	}
	return int(n), nil
}
