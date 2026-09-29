package gateway

import (
	"flag"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestListenAddressUsesConfiguredValue(t *testing.T) {
	set := flag.NewFlagSet("websocket", flag.ContinueOnError)
	set.String("address", "", "")
	if err := set.Set("address", ":4321"); err != nil {
		t.Fatalf("set address: %v", err)
	}
	ctx := cli.NewContext(cli.NewApp(), set, nil)

	if got := listenAddress(ctx); got != ":4321" {
		t.Fatalf("unexpected listen address: got %q, want %q", got, ":4321")
	}
}

func TestListenAddressUsesDefault(t *testing.T) {
	set := flag.NewFlagSet("websocket", flag.ContinueOnError)
	set.String("address", "", "")
	ctx := cli.NewContext(cli.NewApp(), set, nil)

	if got := listenAddress(ctx); got != address {
		t.Fatalf("unexpected default listen address: got %q, want %q", got, address)
	}
}
