package main

import "testing"

func TestAppArguments(t *testing.T) {
	if len(appArguments(false)) != 0 {
		t.Fatal("opening window must not start login")
	}
	args := appArguments(true)
	if len(args) != 1 || args[0] != "--login" {
		t.Fatalf("login args: %v", args)
	}
}
