package iroh_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nerves-hub/nh/internal/iroh"
	"github.com/nerves-hub/nh/internal/irohconsole"
)

// TestLiveIrohConsole exercises the real shipping packages (iroh.Dial +
// irohconsole.Connect) against a real device. Opt-in: it needs a reachable
// device and credentials, so it is skipped unless NH_IROH_LIVE=1.
//
//	NH_IROH_LIVE=1 NH_IROH_TICKET=endpoint… NH_IROH_AUTH=secret \
//	    go test ./internal/iroh -run TestLiveIrohConsole -v -count=1
func TestLiveIrohConsole(t *testing.T) {
	if os.Getenv("NH_IROH_LIVE") != "1" {
		t.Skip("set NH_IROH_LIVE=1 with NH_IROH_TICKET and NH_IROH_AUTH to run")
	}
	ticket := os.Getenv("NH_IROH_TICKET")
	if ticket == "" {
		t.Fatal("NH_IROH_TICKET is required")
	}
	auth := os.Getenv("NH_IROH_AUTH")

	sk, err := iroh.LoadOrCreateIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, cleanup, err := iroh.Dial(ctx, ticket, sk, irohconsole.ALPN)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cleanup()

	session, err := irohconsole.Connect(stream, irohconsole.StaticResponder(auth))
	if err != nil {
		t.Fatalf("connect/auth: %v", err)
	}
	defer session.Close()

	out, err := session.Output()
	if err != nil {
		t.Fatalf("reading device output: %v", err)
	}
	t.Logf("HANDSHAKE OK — device sent %d bytes: %q", len(out), out)
}
