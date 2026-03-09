package sitegen

import "testing"

func TestDictEvenPairs(t *testing.T) {
	got, err := dict("a", 1, "b", "two")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["a"] != 1 || got["b"] != "two" {
		t.Fatalf("unexpected map values: %#v", got)
	}
}

func TestDictOddPairsError(t *testing.T) {
	if _, err := dict("a"); err == nil {
		t.Fatal("expected error for odd argument count")
	}
}
