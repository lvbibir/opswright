package natsort

import (
	"slices"
	"testing"
)

// TestCompare covers the three things Compare orders by: runs of digits
// as numbers ("COM2" before "COM10", and "1.1.1.2" before "1.1.1.10"),
// equal runs by their leading zeros ("COM2" before "COM02"), and every
// other byte as itself ("/" before "1" before "C").
func TestCompare(t *testing.T) {
	got := []string{"COM10", "COM2", "COM1", "/dev/ttyUSB10", "/dev/ttyUSB2", "/dev/ttyACM0", "COM02", "/dev/ttyUSB", "1.1.1.10", "1.1.1.2"}
	slices.SortFunc(got, Compare)
	want := []string{"/dev/ttyACM0", "/dev/ttyUSB", "/dev/ttyUSB2", "/dev/ttyUSB10", "1.1.1.2", "1.1.1.10", "COM1", "COM2", "COM02", "COM10"}
	if !slices.Equal(got, want) {
		t.Fatalf("sorted = %q, want %q", got, want)
	}
}
