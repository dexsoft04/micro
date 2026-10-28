package cmd

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"
)

func TestVersionOutputIncludesExecutablePlatform(t *testing.T) {
	c := New()
	var output bytes.Buffer
	c.app.Writer = &output
	if err := c.app.Run([]string{"mcbeam", "--version"}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("mcbeam version %s %s/%s\n", buildVersion(), runtime.GOOS, runtime.GOARCH)
	if output.String() != want {
		t.Fatalf("version output = %q, want %q", output.String(), want)
	}
	if c.app.Version != buildVersion() {
		t.Fatalf("application version changed: %q", c.app.Version)
	}
}
