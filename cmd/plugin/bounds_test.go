package main

import (
	"math"
	"testing"
)

func TestBoundedLenRejectsOverflowAndHuge(t *testing.T) {
	if _, err := boundedLen(uint64(maxHostRequestBytes)+1, maxHostRequestBytes); err == nil {
		t.Fatal("expected error for request over cap")
	}
	if _, err := boundedLen(uint64(math.MaxInt32)+1, math.MaxInt32); err == nil {
		t.Fatal("expected error for size_t that does not fit C.int")
	}
	n, err := boundedLen(16, maxHostRequestBytes)
	if err != nil || n != 16 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, err = boundedLen(0, maxHostRequestBytes)
	if err != nil || n != 0 {
		t.Fatalf("zero n=%d err=%v", n, err)
	}
}
