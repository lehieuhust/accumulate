package simulator

//lint:file-ignore ST1001 Don't care

import (
	"os"

	"github.com/stretchr/testify/require"
	. "gitlab.com/accumulatenetwork/accumulate/internal/block"
	"gitlab.com/accumulatenetwork/accumulate/internal/chain"
	"gitlab.com/accumulatenetwork/accumulate/internal/database"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
)

func InitFromSnapshot(t TB, db database.Beginner, exec *Executor, filename string) {
	t.Helper()

	f, err := os.Open(filename)
	require.NoError(tb{t}, err)
	defer f.Close()
	batch := db.Begin(true)
	defer batch.Discard()
	require.NoError(tb{t}, exec.RestoreSnapshot(batch, f))
	require.NoError(tb{t}, batch.Commit())
}

func NormalizeEnvelope(t TB, envelope *protocol.Envelope) []*chain.Delivery {
	t.Helper()

	deliveries, err := chain.NormalizeEnvelope(envelope)
	require.NoError(tb{t}, err)
	return deliveries
}
