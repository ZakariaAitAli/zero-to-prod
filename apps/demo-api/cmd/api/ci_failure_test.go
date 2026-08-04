package main

import "testing"

func TestIntentionalCIFailure(t *testing.T) {
	t.Fatal("intentional failure to verify CI behavior")
}
