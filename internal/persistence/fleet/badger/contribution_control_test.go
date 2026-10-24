package badger

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/contribution"
	"testing"
	"time"
)

func TestContributionControlCanonicalDigestDeniesCorruption(t *testing.T) {
	c := contribution.Control{QueueItemID: "q", ContractDigest: contribution.Digest([]byte("contract")), OwnerID: "owner", SessionID: "current-session", Sequence: 1, Delivery: "local-only", At: time.Now().UTC()}
	b, _ := json.Marshal(c)
	c.Digest = contribution.Digest(b)
	wire, _ := json.Marshal(c)
	if _, err := decodeContributionControl(wire); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func() []byte{
		func() []byte { x := c; x.Stop = true; b, _ := json.Marshal(x); return b },
		func() []byte { return append([]byte(" "), wire...) },
		func() []byte { return append(wire[:len(wire)-1], []byte(",\"unknown\":true}")...) },
		func() []byte { return []byte("null") },
	} {
		if _, err := decodeContributionControl(mutate()); err == nil {
			t.Fatal("corrupt control accepted")
		}
	}
}
