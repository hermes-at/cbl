package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestVersionReportsRelease(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	if err := run(context.Background(), []string{"version"}); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdout = old
	output, _ := io.ReadAll(r)
	_ = r.Close()
	if strings.TrimSpace(string(output)) != "cbl v0.3.2-dev" {
		t.Fatalf("version=%q", output)
	}
}
