package derivation

import (
	"bytes"
	"fmt"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/specularl2/specular/clients/geth/specular/rollup/types"
	"github.com/specularl2/specular/clients/geth/specular/utils/log"
)

type batchBuilder struct {
	maxBatchSize uint64

	pendingBlocks   []DerivationBlock
	builtBatchAttrs *BatchAttributes

	lastAppended types.BlockID
}

type BlockAttributes struct {
	timestamp *big.Int
	txs       [][]byte // encoded batch of transactions.
}

func (a *BlockAttributes) Timestamp() *big.Int { return a.timestamp }
func (a *BlockAttributes) Txs() [][]byte       { return a.txs }

type BatchAttributes struct {
	firstL2BlockNumber *big.Int
	blocks             []BlockAttributes
}

// TODO: find a better place to not hardcode this
func (a *BatchAttributes) TxBatchVersion() *big.Int     { return big.NewInt(0) }
func (a *BatchAttributes) FirstL2BlockNumber() *big.Int { return a.firstL2BlockNumber }
func (a *BatchAttributes) Blocks() []BlockAttributes    { return a.blocks }

type HeaderRef interface {
	GetHash() common.Hash
	GetParentHash() common.Hash
}

type InvalidBlockError struct{ Msg string }

func (e InvalidBlockError) Error() string { return e.Msg }

// maxBatchSize is a soft cap on the size of the batch (# of bytes).
func NewBatchBuilder(maxBatchSize uint64) (*batchBuilder, error) {
	return &batchBuilder{maxBatchSize: maxBatchSize}, nil
}

func (b *batchBuilder) LastAppended() types.BlockID { return b.lastAppended }

// Appends a block, to be processed and batched.
// Returns a `InvalidBlockError` if the block is not a child of the last appended block.
func (b *batchBuilder) Append(block DerivationBlock, header HeaderRef) error {
	// Ensure block is a child of the last appended block. Not enforced when no prior blocks.
	if (header.GetParentHash() != b.lastAppended.GetHash()) && (b.lastAppended.GetHash() != common.Hash{}) {
		return InvalidBlockError{Msg: "Appended block is not a child of the last appended block"}
	}
	b.pendingBlocks = append(b.pendingBlocks, block)
	b.lastAppended = types.NewBlockID(block.BlockNumber(), header.GetHash())
	return nil
}

func (b *batchBuilder) Reset(lastAppended types.BlockID) {
	b.pendingBlocks = []DerivationBlock{}
	b.builtBatchAttrs = nil
	b.lastAppended = lastAppended
}

// This short-circuits the build process if a batch is
// already built and `Advance` hasn't been called.
func (b *batchBuilder) Build() (*BatchAttributes, error) {
	if b.builtBatchAttrs != nil {
		return b.builtBatchAttrs, nil
	}
	if len(b.pendingBlocks) == 0 {
		return nil, io.EOF
	}
	batchAttrs, err := b.serializeToAttrs()
	if err != nil {
		return nil, fmt.Errorf("failed to build batch: %w", err)
	}
	b.builtBatchAttrs = batchAttrs
	return batchAttrs, nil
}

func (b *batchBuilder) Advance() {
	b.builtBatchAttrs = nil
}

func (b *batchBuilder) serializeToAttrs() (*BatchAttributes, error) {
	var (
		buf    = new(bytes.Buffer)
		block  DerivationBlock
		idx    int
		blocks []BlockAttributes
	)

	firstL2BlockNumber := big.NewInt(0).SetUint64(b.pendingBlocks[0].BlockNumber())
	numBytes := rlp.IntSize(firstL2BlockNumber.Uint64())

	// Iterate block-by-block to enforce soft cap on batch size.
	for idx, block = range b.pendingBlocks {
		blockTimestamp := big.NewInt(0).SetUint64(block.Timestamp())
		// Construct txData.
		for _, tx := range block.Txs() {
			numBytes = numBytes + rlp.IntSize(blockTimestamp.Uint64()) + len(tx)
		}

		// Enforce cap on batch size.
		if uint64(numBytes) > b.maxBatchSize {
			log.Info("Reached max batch size", "numBytes", numBytes, "maxBatchSize", b.maxBatchSize, "numBlocks", len(blocks))
			break
		}

		blocks = append(blocks, BlockAttributes{firstL2BlockNumber, block.Txs()})
		buf.Reset()
	}
	// Construct batch attributes.
	attrs := &BatchAttributes{firstL2BlockNumber, blocks}

	log.Info("Serialized l2 blocks", "first", firstL2BlockNumber, "last", b.pendingBlocks[idx].BlockNumber())
	// Advance queue.
	b.pendingBlocks = b.pendingBlocks[idx+1:]
	log.Trace("Advanced pending blocks", "len", len(b.pendingBlocks))
	return attrs, nil
}
