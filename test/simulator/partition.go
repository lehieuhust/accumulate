package simulator

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/tendermint/tendermint/abci/types"
	"gitlab.com/accumulatenetwork/accumulate/internal/accumulated"
	"gitlab.com/accumulatenetwork/accumulate/internal/block"
	"gitlab.com/accumulatenetwork/accumulate/internal/chain"
	"gitlab.com/accumulatenetwork/accumulate/internal/core"
	"gitlab.com/accumulatenetwork/accumulate/internal/database"
	"gitlab.com/accumulatenetwork/accumulate/internal/errors"
	ioutil2 "gitlab.com/accumulatenetwork/accumulate/internal/ioutil"
	"gitlab.com/accumulatenetwork/accumulate/internal/logging"
	"gitlab.com/accumulatenetwork/accumulate/internal/sortutil"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
	"golang.org/x/sync/errgroup"
)

type Partition struct {
	protocol.PartitionInfo
	logger     logging.OptionalLogger
	nodes      []*Node
	validators [][32]byte

	mu         *sync.Mutex
	mempool    []*chain.Delivery
	deliver    []*chain.Delivery
	blockIndex uint64
	blockTime  time.Time

	submitHook SubmitHookFunc
}

type SubmitHookFunc func(*chain.Delivery) (dropTx, keepHook bool)

type validatorUpdate struct {
	key [32]byte
	typ core.ValidatorUpdate
}

func newPartition(s *Simulator, partition protocol.PartitionInfo) *Partition {
	p := new(Partition)
	p.PartitionInfo = partition
	p.logger.Set(s.logger, "partition", partition.ID)
	p.mu = new(sync.Mutex)
	return p
}

func newBvn(s *Simulator, init *accumulated.BvnInit) (*Partition, error) {
	p := newPartition(s, protocol.PartitionInfo{
		ID:   init.Id,
		Type: protocol.PartitionTypeBlockValidator,
	})

	for _, node := range init.Nodes {
		n, err := newNode(s, p, len(p.nodes), node)
		if err != nil {
			return nil, errors.Wrap(errors.StatusUnknownError, err)
		}
		p.nodes = append(p.nodes, n)
	}
	return p, nil
}

func newDn(s *Simulator, init *accumulated.NetworkInit) (*Partition, error) {
	p := newPartition(s, protocol.PartitionInfo{
		ID:   protocol.Directory,
		Type: protocol.PartitionTypeDirectory,
	})

	for _, init := range init.Bvns {
		for _, init := range init.Nodes {
			n, err := newNode(s, p, len(p.nodes), init)
			if err != nil {
				return nil, errors.Wrap(errors.StatusUnknownError, err)
			}
			p.nodes = append(p.nodes, n)
		}
	}
	return p, nil
}

func (p *Partition) View(fn func(*database.Batch) error) error { return p.nodes[0].View(fn) }

func (p *Partition) Update(fn func(*database.Batch) error) error {
	for i, n := range p.nodes {
		err := n.Update(fn)
		if err != nil {
			if i > 0 {
				panic("update succeeded on one node and failed on another")
			}
			return err
		}
	}
	return nil
}

func (p *Partition) SetSubmitHook(fn SubmitHookFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitHook = fn
}

func (p *Partition) initChain(snapshot ioutil2.SectionReader) error {
	results := make([][]byte, len(p.nodes))
	for i, n := range p.nodes {
		_, err := snapshot.Seek(0, io.SeekStart)
		if err != nil {
			return errors.Format(errors.StatusUnknownError, "reset snapshot file: %w", err)
		}
		results[i], err = n.initChain(snapshot)
		if err != nil {
			return errors.Format(errors.StatusUnknownError, "init chain: %w", err)
		}
	}
	for _, v := range results[1:] {
		if !bytes.Equal(results[0], v) {
			return errors.Format(errors.StatusFatalError, "consensus failure: init chain: expected %x, got %x", results[0], v)
		}
	}
	return nil
}

func copyDelivery(d *chain.Delivery) *chain.Delivery {
	e := new(chain.Delivery)
	e.Transaction = d.Transaction.Copy()
	e.Signatures = make([]protocol.Signature, len(d.Signatures))
	for i, s := range d.Signatures {
		e.Signatures[i] = s.CopyAsInterface().(protocol.Signature)
	}
	return e
}

func (p *Partition) Submit(delivery *chain.Delivery, pretend bool) (*protocol.TransactionStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.submitHook != nil {
		drop, keep := p.submitHook(delivery)
		if !keep {
			p.submitHook = nil
		}
		if drop {
			s := new(protocol.TransactionStatus)
			s.TxID = delivery.Transaction.ID()
			return s, nil
		}
	}

	var err error
	result := make([]*protocol.TransactionStatus, len(p.nodes))
	for i, node := range p.nodes {
		// Make a copy to prevent changes
		result[i], err = node.checkTx(copyDelivery(delivery), types.CheckTxType_New)
		if err != nil {
			return nil, errors.Wrap(errors.StatusFatalError, err)
		}
	}
	for _, r := range result[1:] {
		if !result[0].Equal(r) {
			return nil, errors.Format(errors.StatusFatalError, "consensus failure: check tx: transaction %x (%v)", delivery.Transaction.GetHash()[:4], delivery.Transaction.Body.Type())
		}
	}

	if !pretend && result[0].Code.Success() {
		p.mempool = append(p.mempool, delivery)
	}
	return result[0], nil
}

func (p *Partition) execute(background *errgroup.Group) error {
	// TODO: Limit how many transactions are added to the block? Call recheck?
	p.mu.Lock()
	deliveries := p.deliver
	p.deliver = p.mempool
	p.mempool = nil
	p.mu.Unlock()

	// Initialize block index
	if p.blockIndex > 0 {
		p.blockIndex++
		p.blockTime = p.blockTime.Add(time.Second)
	} else {
		err := p.View(func(batch *database.Batch) error {
			record := batch.Account(protocol.PartitionUrl(p.ID).JoinPath(protocol.Ledger))
			c, err := record.RootChain().Index().Get()
			if err != nil {
				return errors.Format(errors.StatusFatalError, "load root index chain: %w", err)
			}
			entry := new(protocol.IndexEntry)
			err = c.EntryAs(c.Height()-1, entry)
			if err != nil {
				return errors.Format(errors.StatusFatalError, "load root index chain entry 0: %w", err)
			}
			p.blockIndex = entry.BlockIndex + 1
			p.blockTime = entry.BlockTime.Add(time.Second)
			return nil
		})
		if err != nil {
			return err
		}
	}
	p.logger.Debug("Stepping", "block", p.blockIndex)

	// Begin block
	leader := int(p.blockIndex) % len(p.nodes)
	blocks := make([]*block.Block, len(p.nodes))
	for i, n := range p.nodes {
		b := new(block.Block)
		b.Index = p.blockIndex
		b.Time = p.blockTime
		b.IsLeader = i == leader
		blocks[i] = b

		n.executor.Background = func(f func()) { background.Go(func() error { f(); return nil }) }

		err := n.beginBlock(b)
		if err != nil {
			return errors.Format(errors.StatusFatalError, "execute: %w", err)
		}
	}

	// Deliver Tx
	var err error
	results := make([]*protocol.TransactionStatus, len(p.nodes))
	for _, delivery := range deliveries {
		for i, node := range p.nodes {
			// Make a copy to prevent changes
			results[i], err = node.deliverTx(blocks[i], copyDelivery(delivery))
			if err != nil {
				return errors.Format(errors.StatusFatalError, "execute: %w", err)
			}
		}
		for _, r := range results[1:] {
			if !results[0].Equal(r) {
				return errors.Format(errors.StatusFatalError, "consensus failure: deliver tx: transaction %x (%v)", delivery.Transaction.GetHash()[:4], delivery.Transaction.Body.Type())
			}
		}
	}

	// End block
	endBlock := make([][]*validatorUpdate, len(p.nodes))
	for i, n := range p.nodes {
		endBlock[i], err = n.endBlock(blocks[i])
		if err != nil {
			return errors.Format(errors.StatusFatalError, "execute: %w", err)
		}
	}
	for _, v := range endBlock[1:] {
		if len(v) != len(endBlock[0]) {
			return errors.Format(errors.StatusFatalError, "consensus failure: end block")
		}
		for i, v := range v {
			if *v != *endBlock[0][i] {
				return errors.Format(errors.StatusFatalError, "consensus failure: end block")
			}
		}
	}

	// Update validators
	for _, update := range endBlock[0] {
		switch update.typ {
		case core.ValidatorUpdateAdd:
			ptr, new := sortutil.BinaryInsert(&p.validators, func(k [32]byte) int { return bytes.Compare(k[:], update.key[:]) })
			if new {
				*ptr = update.key
			}

		case core.ValidatorUpdateRemove:
			i, found := sortutil.Search(p.validators, func(k [32]byte) int { return bytes.Compare(k[:], update.key[:]) })
			if found {
				sortutil.RemoveAt(&p.validators, i)
			}

		default:
			panic(fmt.Errorf("unknown validator update type %v", update.typ))
		}
	}

	// Commit
	commit := make([][]byte, len(p.nodes))
	for i, n := range p.nodes {
		commit[i], err = n.commit(blocks[i])
		if err != nil {
			return errors.Format(errors.StatusFatalError, "execute: %w", err)
		}
	}
	for _, v := range commit[1:] {
		if !bytes.Equal(commit[0], v) {
			return errors.Format(errors.StatusFatalError, "consensus failure: commit: expected %x, got %x", commit[0], v)
		}
	}

	return nil
}
