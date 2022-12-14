package e2e

import (
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gitlab.com/accumulatenetwork/accumulate/internal/chain"
	acctesting "gitlab.com/accumulatenetwork/accumulate/internal/testing"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	. "gitlab.com/accumulatenetwork/accumulate/protocol"
	. "gitlab.com/accumulatenetwork/accumulate/test/helpers"
	"gitlab.com/accumulatenetwork/accumulate/test/simulator"
)

// TestDnStall verifies that a BVN can detect if the DN stalls
func TestDnStall(t *testing.T) {
	alice := url.MustParse("alice")
	bob := url.MustParse("bob")
	aliceKey := acctesting.GenerateKey(alice)
	bobKey := acctesting.GenerateKey(bob)
	sim := NewSim(t,
		simulator.MemoryDatabase,
		simulator.SimpleNetwork(t.Name(), 3, 3),
		simulator.Genesis(GenesisTime),
	)
	sim.SetRoute(alice, "BVN0")

	// Generate some history
	MakeIdentity(t, sim.DatabaseFor(alice), alice, aliceKey[32:])
	CreditCredits(t, sim.DatabaseFor(alice), alice.JoinPath("book", "1"), 1e9)
	MakeAccount(t, sim.DatabaseFor(alice), &TokenAccount{Url: alice.JoinPath("tokens"), TokenUrl: AcmeUrl()})
	CreditTokens(t, sim.DatabaseFor(alice), alice.JoinPath("tokens"), big.NewInt(1e12))
	MakeIdentity(t, sim.DatabaseFor(bob), bob, bobKey[32:])
	MakeAccount(t, sim.DatabaseFor(bob), &TokenAccount{Url: bob.JoinPath("tokens"), TokenUrl: AcmeUrl()})

	st := sim.SubmitSuccessfully(
		acctesting.NewTransaction().
			WithPrincipal(alice.JoinPath("tokens")).
			WithSigner(alice.JoinPath("book", "1"), 1).
			WithTimestamp(1).
			WithBody(&SendTokens{To: []*TokenRecipient{{Url: bob.JoinPath("tokens"), Amount: *big.NewInt(123)}}}).
			Initiate(SignatureTypeED25519, aliceKey).
			BuildDelivery())

	sim.StepUntil(
		Txn(st.TxID).Succeeds(),
		Txn(st.TxID).Produced().Succeeds())

	// Drop all transactions from the DN
	for _, p := range sim.Partitions() {
		sim.SetSubmitHook(p.ID, func(delivery *chain.Delivery) (dropTx bool, keepHook bool) {
			for _, sig := range delivery.Signatures {
				sig, ok := sig.(*PartitionSignature)
				if !ok {
					continue
				}
				srcpart, ok := ParsePartitionUrl(sig.SourceNetwork)
				if !ok {
					continue
				}
				if strings.EqualFold(srcpart, Directory) {
					return true, true
				}
			}
			return false, true
		})
	}

	// Trigger another block
	sim.SubmitSuccessfully(
		acctesting.NewTransaction().
			WithPrincipal(alice.JoinPath("tokens")).
			WithSigner(alice.JoinPath("book", "1"), 1).
			WithTimestamp(2).
			WithBody(&SendTokens{To: []*TokenRecipient{{Url: bob.JoinPath("tokens"), Amount: *big.NewInt(123)}}}).
			Initiate(SignatureTypeED25519, aliceKey).
			BuildDelivery())

	// Run some blocks
	for i := 0; i < 50; i++ {
		sim.Step()
	}

	// Verify that the number of acknowledged anchors is less than the number
	// produced
	ledger := GetAccount[*AnchorLedger](t, sim.Database("BVN0"), PartitionUrl("BVN0").JoinPath(AnchorPool)).Partition(DnUrl())
	require.Less(t, ledger.Acknowledged, ledger.Produced)
}
