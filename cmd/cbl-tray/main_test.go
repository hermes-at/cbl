package main

import (
	"strings"
	"testing"
)

func TestProgressLineBounds(t *testing.T) {
	for _, test := range []struct {
		used int
		want string
	}{{-1, "0% used"}, {0, "0% used"}, {100, "100% used"}, {150, "100% used"}} {
		if got := progressLine(100-test.used, test.used); !strings.Contains(got, test.want) {
			t.Fatalf("got %q want %q", got, test.want)
		}
	}
}
