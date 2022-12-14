package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gitlab.com/accumulatenetwork/accumulate/internal/chain"
	"gitlab.com/accumulatenetwork/accumulate/internal/database"
	acctesting "gitlab.com/accumulatenetwork/accumulate/internal/testing"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	. "gitlab.com/accumulatenetwork/accumulate/protocol"
	. "gitlab.com/accumulatenetwork/accumulate/test/helpers"
	"gitlab.com/accumulatenetwork/accumulate/test/simulator"
)

// TestAnchorThreshold verifies that an anchor is not executed until it is
// signed by 2/3 of the validators
func TestAnchorThreshold(t *testing.T) {
	const bvnCount, valCount = 1, 3 // One BVN, three nodes
	alice := url.MustParse("alice")
	aliceKey := acctesting.GenerateKey(alice)
	sim := NewSim(t,
		simulator.MemoryDatabase,
		simulator.SimpleNetwork(t.Name(), bvnCount, valCount),
		simulator.Genesis(GenesisTime),
	)
	sim.SetRoute(alice, "BVN0")

	// Clear out the anchors from genesis
	sim.StepN(50)

	// Do something
	MakeIdentity(t, sim.DatabaseFor(alice), alice, aliceKey[32:])
	CreditCredits(t, sim.DatabaseFor(alice), alice.JoinPath("book", "1"), 1e9)

	sim.SubmitSuccessfully(
		acctesting.NewTransaction().
			WithPrincipal(alice).
			WithSigner(alice.JoinPath("book", "1"), 1).
			WithTimestamp(1).
			WithBody(&CreateTokenAccount{Url: alice.JoinPath("tokens"), TokenUrl: AcmeUrl()}).
			Initiate(SignatureTypeED25519, aliceKey).
			BuildDelivery())

	// Capture the BVN's anchors and verify they're the same
	var anchors []*chain.Delivery
	sim.SetSubmitHook("Directory", func(delivery *chain.Delivery) (dropTx bool, keepHook bool) {
		if delivery.Transaction.Body.Type() != TransactionTypeBlockValidatorAnchor {
			return false, true
		}
		anchors = append(anchors, delivery)
		return true, len(anchors) < valCount
	})

	for len(anchors) < valCount {
		sim.Step()
	}

	txid := anchors[0].Transaction.ID()
	for _, anchor := range anchors[1:] {
		require.True(t, anchors[0].Transaction.Equal(anchor.Transaction))
	}

	// Verify the anchor was captured
	View(t, sim.Database("BVN0"), func(batch *database.Batch) {
		hash := txid.Hash()
		v, err := batch.Transaction(hash[:]).Status().Get()
		require.NoError(t, err)
		require.True(t, v.Equal(new(TransactionStatus)))
	})

	// Submit one signature and verify it is pending
	sim.SubmitSuccessfully(anchors[0])
	sim.StepUntil(Txn(txid).IsPending())

	// Re-submit the first signature and verify it is still pending
	sim.SubmitSuccessfully(anchors[0])
	sim.StepN(50)
	require.True(t, sim.GetStatus(txid).Pending())

	// Submit a second signature and verify it is delivered
	sim.SubmitSuccessfully(anchors[1])
	sim.StepUntil(Txn(txid).Succeeds())
}
