package batcher

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"testing"

	"github.com/DougNorm/optimism/op-batcher/compressor"
	"github.com/DougNorm/optimism/op-node/rollup"
	"github.com/DougNorm/optimism/op-node/rollup/derive"
	dtest "github.com/DougNorm/optimism/op-node/rollup/derive/test"
	"github.com/DougNorm/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"

	"github.com/stretchr/testify/require"
)

var defaultTestChannelConfig = ChannelConfig{
	SeqWindowSize:      15,
	ChannelTimeout:     40,
	MaxChannelDuration: 1,
	SubSafetyMargin:    4,
	MaxFrameSize:       120000,
	CompressorConfig: compressor.Config{
		TargetFrameSize:  100000,
		TargetNumFrames:  1,
		ApproxComprRatio: 0.4,
	},
	BatchType: derive.SingularBatchType,
}

var defaultTestRollupConfig = rollup.Config{
	Genesis:   rollup.Genesis{L2: eth.BlockID{Number: 0}},
	L2ChainID: big.NewInt(1234),
}

// TestChannelConfig_Check tests the [ChannelConfig] [Check] function.
func TestChannelConfig_Check(t *testing.T) {
	type test struct {
		input     ChannelConfig
		assertion func(error)
	}

	// Construct test cases that test the boundary conditions
	zeroChannelConfig := defaultTestChannelConfig
	zeroChannelConfig.MaxFrameSize = 0
	timeoutChannelConfig := defaultTestChannelConfig
	timeoutChannelConfig.ChannelTimeout = 0
	timeoutChannelConfig.SubSafetyMargin = 1
	tests := []test{
		{
			input: defaultTestChannelConfig,
			assertion: func(output error) {
				require.NoError(t, output)
			},
		},
		{
			input: timeoutChannelConfig,
			assertion: func(output error) {
				require.ErrorIs(t, output, ErrInvalidChannelTimeout)
			},
		},
		{
			input: zeroChannelConfig,
			assertion: func(output error) {
				require.EqualError(t, output, "max frame size cannot be zero")
			},
		},
	}
	for i := 1; i < derive.FrameV0OverHeadSize; i++ {
		smallChannelConfig := defaultTestChannelConfig
		smallChannelConfig.MaxFrameSize = uint64(i)
		expectedErr := fmt.Sprintf("max frame size %d is less than the minimum 23", i)
		tests = append(tests, test{
			input: smallChannelConfig,
			assertion: func(output error) {
				require.EqualError(t, output, expectedErr)
			},
		})
	}

	// Run the table tests
	for _, test := range tests {
		test.assertion(test.input.Check())
	}
}

// FuzzChannelConfig_CheckTimeout tests the [ChannelConfig] [Check] function
// with fuzzing to make sure that a [ErrInvalidChannelTimeout] is thrown when
// the [ChannelTimeout] is less than the [SubSafetyMargin].
func FuzzChannelConfig_CheckTimeout(f *testing.F) {
	for i := range [10]int{} {
		f.Add(uint64(i+1), uint64(i))
	}
	f.Fuzz(func(t *testing.T, channelTimeout uint64, subSafetyMargin uint64) {
		// We only test where [ChannelTimeout] is less than the [SubSafetyMargin]
		// So we cannot have [ChannelTimeout] be [math.MaxUint64]
		if channelTimeout == math.MaxUint64 {
			channelTimeout = math.MaxUint64 - 1
		}
		if subSafetyMargin <= channelTimeout {
			subSafetyMargin = channelTimeout + 1
		}

		channelConfig := defaultTestChannelConfig
		channelConfig.ChannelTimeout = channelTimeout
		channelConfig.SubSafetyMargin = subSafetyMargin
		require.ErrorIs(t, channelConfig.Check(), ErrInvalidChannelTimeout)
	})
}

// addMiniBlock adds a minimal valid L2 block to the channel builder using the
// channelBuilder.AddBlock method.
func addMiniBlock(cb *channelBuilder) error {
	a := newMiniL2Block(0)
	_, err := cb.AddBlock(a)
	return err
}

// newMiniL2Block returns a minimal L2 block with a minimal valid L1InfoDeposit
// transaction as first transaction. Both blocks are minimal in the sense that
// most fields are left at defaults or are unset.
//
// If numTx > 0, that many empty DynamicFeeTxs will be added to the txs.
func newMiniL2Block(numTx int) *types.Block {
	return newMiniL2BlockWithNumberParent(numTx, new(big.Int), (common.Hash{}))
}

// newMiniL2Block returns a minimal L2 block with a minimal valid L1InfoDeposit
// transaction as first transaction. Both blocks are minimal in the sense that
// most fields are left at defaults or are unset. Block number and parent hash
// will be set to the given parameters number and parent.
//
// If numTx > 0, that many empty DynamicFeeTxs will be added to the txs.
func newMiniL2BlockWithNumberParent(numTx int, number *big.Int, parent common.Hash) *types.Block {
	l1Block := types.NewBlock(&types.Header{
		BaseFee:    big.NewInt(10),
		Difficulty: common.Big0,
		Number:     big.NewInt(100),
	}, nil, nil, nil, trie.NewStackTrie(nil))
	l1InfoTx, err := derive.L1InfoDeposit(0, eth.BlockToInfo(l1Block), eth.SystemConfig{}, false)
	if err != nil {
		panic(err)
	}

	txs := make([]*types.Transaction, 0, 1+numTx)
	txs = append(txs, types.NewTx(l1InfoTx))
	for i := 0; i < numTx; i++ {
		txs = append(txs, types.NewTx(&types.DynamicFeeTx{}))
	}

	return types.NewBlock(&types.Header{
		Number:     number,
		ParentHash: parent,
	}, txs, nil, nil, trie.NewStackTrie(nil))
}

// addTooManyBlocks adds blocks to the channel until it hits an error,
// which is presumably ErrTooManyRLPBytes.
func addTooManyBlocks(cb *channelBuilder) error {
	rng := rand.New(rand.NewSource(1234))
	for i := 0; i < 10_000; i++ {
		block := dtest.RandomL2BlockWithChainId(rng, 1000, defaultTestRollupConfig.L2ChainID)
		_, err := cb.AddBlock(block)
		if err != nil {
			return err
		}
	}

	return nil
}

// FuzzDurationTimeoutZeroMaxChannelDuration ensures that when whenever the MaxChannelDuration
// is set to 0, the channel builder cannot have a duration timeout.
func FuzzDurationTimeoutZeroMaxChannelDuration(f *testing.F) {
	for i := range [10]int{} {
		f.Add(uint64(i))
	}
	f.Fuzz(func(t *testing.T, l1BlockNum uint64) {
		channelConfig := defaultTestChannelConfig
		channelConfig.MaxChannelDuration = 0
		cb, err := newChannelBuilder(channelConfig, nil)
		require.NoError(t, err)
		cb.timeout = 0
		cb.updateDurationTimeout(l1BlockNum)
		require.False(t, cb.TimedOut(l1BlockNum))
	})
}

// FuzzChannelBuilder_DurationZero ensures that when whenever the MaxChannelDuration
// is not set to 0, the channel builder will always have a duration timeout
// as long as the channel builder's timeout is set to 0.
func FuzzChannelBuilder_DurationZero(f *testing.F) {
	for i := range [10]int{} {
		f.Add(uint64(i), uint64(i))
	}
	f.Fuzz(func(t *testing.T, l1BlockNum uint64, maxChannelDuration uint64) {
		if maxChannelDuration == 0 {
			t.Skip("Max channel duration cannot be 0")
		}

		// Create the channel builder
		channelConfig := defaultTestChannelConfig
		channelConfig.MaxChannelDuration = maxChannelDuration
		cb, err := newChannelBuilder(channelConfig, nil)
		require.NoError(t, err)

		// Whenever the timeout is set to 0, the channel builder should have a duration timeout
		cb.timeout = 0
		cb.updateDurationTimeout(l1BlockNum)
		cb.checkTimeout(l1BlockNum + maxChannelDuration)
		require.ErrorIs(t, cb.FullErr(), ErrMaxDurationReached)
	})
}

// FuzzDurationTimeoutMaxChannelDuration ensures that when whenever the MaxChannelDuration
// is not set to 0, the channel builder will always have a duration timeout
// as long as the channel builder's timeout is greater than the target block number.
func FuzzDurationTimeoutMaxChannelDuration(f *testing.F) {
	// Set multiple seeds in case fuzzing isn't explicitly used
	for i := range [10]int{} {
		f.Add(uint64(i), uint64(i), uint64(i))
	}
	f.Fuzz(func(t *testing.T, l1BlockNum uint64, maxChannelDuration uint64, timeout uint64) {
		if maxChannelDuration == 0 {
			t.Skip("Max channel duration cannot be 0")
		}

		// Create the channel builder
		channelConfig := defaultTestChannelConfig
		channelConfig.MaxChannelDuration = maxChannelDuration
		cb, err := newChannelBuilder(channelConfig, nil)
		require.NoError(t, err)

		// Whenever the timeout is greater than the l1BlockNum,
		// the channel builder should have a duration timeout
		cb.timeout = timeout
		cb.updateDurationTimeout(l1BlockNum)
		if timeout > l1BlockNum+maxChannelDuration {
			// Notice: we cannot call this outside of the if statement
			// because it would put the channel builder in an invalid state.
			// That is, where the channel builder has a value set for the timeout
			// with no timeoutReason. This subsequently causes a panic when
			// a nil timeoutReason is used as an error (eg when calling FullErr).
			cb.checkTimeout(l1BlockNum + maxChannelDuration)
			require.ErrorIs(t, cb.FullErr(), ErrMaxDurationReached)
		} else {
			require.NoError(t, cb.FullErr())
		}
	})
}

// FuzzChannelCloseTimeout ensures that the channel builder has a [ErrChannelTimeoutClose]
// as long as the timeout constraint is met and the builder's timeout is greater than
// the calculated timeout
func FuzzChannelCloseTimeout(f *testing.F) {
	// Set multiple seeds in case fuzzing isn't explicitly used
	for i := range [10]int{} {
		f.Add(uint64(i), uint64(i), uint64(i), uint64(i*5))
	}
	f.Fuzz(func(t *testing.T, l1BlockNum uint64, channelTimeout uint64, subSafetyMargin uint64, timeout uint64) {
		// Create the channel builder
		channelConfig := defaultTestChannelConfig
		channelConfig.ChannelTimeout = channelTimeout
		channelConfig.SubSafetyMargin = subSafetyMargin
		cb, err := newChannelBuilder(channelConfig, nil)
		require.NoError(t, err)

		// Check the timeout
		cb.timeout = timeout
		cb.FramePublished(l1BlockNum)
		calculatedTimeout := l1BlockNum + channelTimeout - subSafetyMargin
		if timeout > calculatedTimeout && calculatedTimeout != 0 {
			cb.checkTimeout(calculatedTimeout)
			require.ErrorIs(t, cb.FullErr(), ErrChannelTimeoutClose)
		} else {
			require.NoError(t, cb.FullErr())
		}
	})
}

// FuzzChannelZeroCloseTimeout ensures that the channel builder has a [ErrChannelTimeoutClose]
// as long as the timeout constraint is met and the builder's timeout is set to zero.
func FuzzChannelZeroCloseTimeout(f *testing.F) {
	// Set multiple seeds in case fuzzing isn't explicitly used
	for i := range [10]int{} {
		f.Add(uint64(i), uint64(i), uint64(i))
	}
	f.Fuzz(func(t *testing.T, l1BlockNum uint64, channelTimeout uint64, subSafetyMargin uint64) {
		// Create the channel builder
		channelConfig := defaultTestChannelConfig
		channelConfig.ChannelTimeout = channelTimeout
		channelConfig.SubSafetyMargin = subSafetyMargin
		cb, err := newChannelBuilder(channelConfig, nil)
		require.NoError(t, err)

		// Check the timeout
		cb.timeout = 0
		cb.FramePublished(l1BlockNum)
		calculatedTimeout := l1BlockNum + channelTimeout - subSafetyMargin
		cb.checkTimeout(calculatedTimeout)
		if cb.timeout != 0 {
			require.ErrorIs(t, cb.FullErr(), ErrChannelTimeoutClose)
		}
	})
}

// FuzzSeqWindowClose ensures that the channel builder has a [ErrSeqWindowClose]
// as long as the timeout constraint is met and the builder's timeout is greater than
// the calculated timeout
func FuzzSeqWindowClose(f *testing.F) {
	// Set multiple seeds in case fuzzing isn't explicitly used
	for i := range [10]int{} {
		f.Add(uint64(i), uint64(i), uint64(i), uint64(i*5))
	}
	f.Fuzz(func(t *testing.T, epochNum uint64, seqWindowSize uint64, subSafetyMargin uint64, timeout uint64) {
		// Create the channel builder
		channelConfig := defaultTestChannelConfig
		channelConfig.SeqWindowSize = seqWindowSize
		channelConfig.SubSafetyMargin = subSafetyMargin
		cb, err := newChannelBuilder(channelConfig, nil)
		require.NoError(t, err)

		// Check the timeout
		cb.timeout = timeout
		cb.updateSwTimeout(&derive.SingularBatch{EpochNum: rollup.Epoch(epochNum)})
		calculatedTimeout := epochNum + seqWindowSize - subSafetyMargin
		if timeout > calculatedTimeout && calculatedTimeout != 0 {
			cb.checkTimeout(calculatedTimeout)
			require.ErrorIs(t, cb.FullErr(), ErrSeqWindowClose)
		} else {
			require.NoError(t, cb.FullErr())
		}
	})
}

// FuzzSeqWindowZeroTimeoutClose ensures that the channel builder has a [ErrSeqWindowClose]
// as long as the timeout constraint is met and the builder's timeout is set to zero.
func FuzzSeqWindowZeroTimeoutClose(f *testing.F) {
	// Set multiple seeds in case fuzzing isn't explicitly used
	for i := range [10]int{} {
		f.Add(uint64(i), uint64(i), uint64(i))
	}
	f.Fuzz(func(t *testing.T, epochNum uint64, seqWindowSize uint64, subSafetyMargin uint64) {
		// Create the channel builder
		channelConfig := defaultTestChannelConfig
		channelConfig.SeqWindowSize = seqWindowSize
		channelConfig.SubSafetyMargin = subSafetyMargin
		cb, err := newChannelBuilder(channelConfig, nil)
		require.NoError(t, err)

		// Check the timeout
		cb.timeout = 0
		cb.updateSwTimeout(&derive.SingularBatch{EpochNum: rollup.Epoch(epochNum)})
		calculatedTimeout := epochNum + seqWindowSize - subSafetyMargin
		cb.checkTimeout(calculatedTimeout)
		if cb.timeout != 0 {
			require.ErrorIs(t, cb.FullErr(), ErrSeqWindowClose, "Sequence window close should be reached")
		}
	})
}

func TestChannelBuilderBatchType(t *testing.T) {
	tests := []struct {
		name string
		f    func(t *testing.T, batchType uint)
	}{
		{"ChannelBuilder_MaxRLPBytesPerChannel", ChannelBuilder_MaxRLPBytesPerChannel},
		{"ChannelBuilder_OutputFramesMaxFrameIndex", ChannelBuilder_OutputFramesMaxFrameIndex},
		{"ChannelBuilder_AddBlock", ChannelBuilder_AddBlock},
		{"ChannelBuilder_Reset", ChannelBuilder_Reset},
		{"ChannelBuilder_PendingFrames_TotalFrames", ChannelBuilder_PendingFrames_TotalFrames},
		{"ChannelBuilder_InputBytes", ChannelBuilder_InputBytes},
		{"ChannelBuilder_OutputBytes", ChannelBuilder_OutputBytes},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name+"_SingularBatch", func(t *testing.T) {
			test.f(t, derive.SingularBatchType)
		})
	}

	for _, test := range tests {
		test := test
		t.Run(test.name+"_SpanBatch", func(t *testing.T) {
			test.f(t, derive.SpanBatchType)
		})
	}
}

// TestChannelBuilder_NextFrame tests calling NextFrame on a ChannelBuilder with only one frame
func TestChannelBuilder_NextFrame(t *testing.T) {
	channelConfig := defaultTestChannelConfig

	// Create a new channel builder
	cb, err := newChannelBuilder(channelConfig, nil)
	require.NoError(t, err)

	// Mock the internals of `channelBuilder.outputFrame`
	// to construct a single frame
	co := cb.co
	var buf bytes.Buffer
	fn, err := co.OutputFrame(&buf, channelConfig.MaxFrameSize)
	require.NoError(t, err)

	// Push one frame into to the channel builder
	expectedTx := txID{chID: co.ID(), frameNumber: fn}
	expectedBytes := buf.Bytes()
	frameData := frameData{
		id: frameID{
			chID:        co.ID(),
			frameNumber: fn,
		},
		data: expectedBytes,
	}
	cb.PushFrame(frameData)

	// There should only be 1 frame in the channel builder
	require.Equal(t, 1, cb.PendingFrames())

	// We should be able to increment to the next frame
	constructedFrame := cb.NextFrame()
	require.Equal(t, expectedTx, constructedFrame.id)
	require.Equal(t, expectedBytes, constructedFrame.data)
	require.Equal(t, 0, cb.PendingFrames())

	// The next call should panic since the length of frames is 0
	require.PanicsWithValue(t, "no next frame", func() { cb.NextFrame() })
}

// TestChannelBuilder_OutputWrongFramePanic tests that a panic is thrown when a frame is pushed with an invalid frame id
func TestChannelBuilder_OutputWrongFramePanic(t *testing.T) {
	channelConfig := defaultTestChannelConfig

	// Construct a channel builder
	cb, err := newChannelBuilder(channelConfig, nil)
	require.NoError(t, err)

	// Mock the internals of `channelBuilder.outputFrame`
	// to construct a single frame
	c, err := channelConfig.CompressorConfig.NewCompressor()
	require.NoError(t, err)
	co, err := derive.NewChannelOut(derive.SingularBatchType, c, nil)
	require.NoError(t, err)
	var buf bytes.Buffer
	fn, err := co.OutputFrame(&buf, channelConfig.MaxFrameSize)
	require.NoError(t, err)

	// The frame push should panic since we constructed a new channel out
	// so the channel out id won't match
	require.PanicsWithValue(t, "wrong channel", func() {
		frame := frameData{
			id: frameID{
				chID:        co.ID(),
				frameNumber: fn,
			},
			data: buf.Bytes(),
		}
		cb.PushFrame(frame)
	})
}

// TestChannelBuilder_OutputFramesWorks tests the [ChannelBuilder] OutputFrames is successful.
func TestChannelBuilder_OutputFramesWorks(t *testing.T) {
	channelConfig := defaultTestChannelConfig
	channelConfig.MaxFrameSize = 24

	// Construct the channel builder
	cb, err := newChannelBuilder(channelConfig, nil)
	require.NoError(t, err)
	require.False(t, cb.IsFull())
	require.Equal(t, 0, cb.PendingFrames())

	// Calling OutputFrames without having called [AddBlock]
	// should return no error
	require.NoError(t, cb.OutputFrames())

	// There should be no ready bytes yet
	require.Equal(t, 0, cb.co.ReadyBytes())

	// Let's add a block
	require.NoError(t, addMiniBlock(cb))
	require.NoError(t, cb.co.Flush())

	// Check how many ready bytes
	// There should be more than the max frame size ready
	require.Greater(t, uint64(cb.co.ReadyBytes()), channelConfig.MaxFrameSize)
	require.Equal(t, 0, cb.PendingFrames())

	// The channel should not be full
	// but we want to output the frames for testing anyways
	require.False(t, cb.IsFull())

	// We should be able to output the frames
	require.NoError(t, cb.OutputFrames())

	// There should be many frames in the channel builder now
	require.Greater(t, cb.PendingFrames(), 1)
	for _, frame := range cb.frames {
		require.Len(t, frame.data, int(channelConfig.MaxFrameSize))
	}
}

// TestChannelBuilder_OutputFramesWorks tests the [ChannelBuilder] OutputFrames is successful.
func TestChannelBuilder_OutputFramesWorks_SpanBatch(t *testing.T) {
	channelConfig := defaultTestChannelConfig
	channelConfig.MaxFrameSize = 24
	channelConfig.CompressorConfig.TargetFrameSize = 50
	channelConfig.BatchType = derive.SpanBatchType

	// Construct the channel builder
	cb, err := newChannelBuilder(channelConfig, &defaultTestRollupConfig)
	require.NoError(t, err)
	require.False(t, cb.IsFull())
	require.Equal(t, 0, cb.PendingFrames())

	// Calling OutputFrames without having called [AddBlock]
	// should return no error
	require.NoError(t, cb.OutputFrames())

	// There should be no ready bytes yet
	require.Equal(t, 0, cb.co.ReadyBytes())

	// fill up
	for {
		err = addMiniBlock(cb)
		if err == nil {
			require.False(t, cb.IsFull())
			// There should be no ready bytes until the channel is full
			require.Equal(t, cb.co.ReadyBytes(), 0)
		} else {
			require.ErrorIs(t, err, derive.CompressorFullErr)
			break
		}
	}

	require.True(t, cb.IsFull())
	// Check how many ready bytes
	// There should be more than the max frame size ready
	require.Greater(t, uint64(cb.co.ReadyBytes()), channelConfig.MaxFrameSize)
	require.Equal(t, 0, cb.PendingFrames())

	// We should be able to output the frames
	require.NoError(t, cb.OutputFrames())

	// There should be many frames in the channel builder now
	require.Greater(t, cb.PendingFrames(), 1)
	for i := 0; i < cb.numFrames-1; i++ {
		require.Len(t, cb.frames[i].data, int(channelConfig.MaxFrameSize))
	}
	require.LessOrEqual(t, len(cb.frames[len(cb.frames)-1].data), int(channelConfig.MaxFrameSize))
}

// ChannelBuilder_MaxRLPBytesPerChannel tests the [channelBuilder.OutputFrames]
// function errors when the max RLP bytes per channel is reached.
func ChannelBuilder_MaxRLPBytesPerChannel(t *testing.T, batchType uint) {
	t.Parallel()
	channelConfig := defaultTestChannelConfig
	channelConfig.MaxFrameSize = derive.MaxRLPBytesPerChannel * 2
	channelConfig.CompressorConfig.TargetFrameSize = derive.MaxRLPBytesPerChannel * 2
	channelConfig.CompressorConfig.ApproxComprRatio = 1
	channelConfig.BatchType = batchType

	// Construct the channel builder
	cb, err := newChannelBuilder(channelConfig, &defaultTestRollupConfig)
	require.NoError(t, err)

	// Add a block that overflows the [ChannelOut]
	err = addTooManyBlocks(cb)
	require.ErrorIs(t, err, derive.ErrTooManyRLPBytes)
}

// ChannelBuilder_OutputFramesMaxFrameIndex tests the [ChannelBuilder.OutputFrames]
// function errors when the max frame index is reached.
func ChannelBuilder_OutputFramesMaxFrameIndex(t *testing.T, batchType uint) {
	channelConfig := defaultTestChannelConfig
	channelConfig.MaxFrameSize = 24
	channelConfig.CompressorConfig.TargetNumFrames = 6000
	channelConfig.CompressorConfig.TargetFrameSize = 24
	channelConfig.CompressorConfig.ApproxComprRatio = 1
	channelConfig.BatchType = batchType

	rng := rand.New(rand.NewSource(123))

	// Continuously add blocks until the max frame index is reached
	// This should cause the [channelBuilder.OutputFrames] function
	// to error
	cb, err := newChannelBuilder(channelConfig, &defaultTestRollupConfig)
	require.NoError(t, err)
	require.False(t, cb.IsFull())
	require.Equal(t, 0, cb.PendingFrames())
	for {
		a := dtest.RandomL2BlockWithChainId(rng, 1, defaultTestRollupConfig.L2ChainID)
		_, err = cb.AddBlock(a)
		if cb.IsFull() {
			fullErr := cb.FullErr()
			require.ErrorIs(t, fullErr, derive.CompressorFullErr)
			break
		}
		require.NoError(t, err)
	}

	_ = cb.OutputFrames()
	require.ErrorIs(t, cb.FullErr(), ErrMaxFrameIndex)
}

// ChannelBuilder_AddBlock tests the AddBlock function
func ChannelBuilder_AddBlock(t *testing.T, batchType uint) {
	channelConfig := defaultTestChannelConfig
	channelConfig.BatchType = batchType

	// Lower the max frame size so that we can batch
	channelConfig.MaxFrameSize = 20

	// Configure the Input Threshold params so we observe a full channel
	channelConfig.CompressorConfig.TargetFrameSize = 20
	channelConfig.CompressorConfig.TargetNumFrames = 2
	channelConfig.CompressorConfig.ApproxComprRatio = 1

	// Construct the channel builder
	cb, err := newChannelBuilder(channelConfig, &defaultTestRollupConfig)
	require.NoError(t, err)

	// Add a nonsense block to the channel builder
	require.NoError(t, addMiniBlock(cb))
	require.NoError(t, cb.co.Flush())

	// Check the fields reset in the AddBlock function
	expectedInputBytes := 74
	if batchType == derive.SpanBatchType {
		expectedInputBytes = 47
	}
	require.Equal(t, expectedInputBytes, cb.co.InputBytes())
	require.Equal(t, 1, len(cb.blocks))
	require.Equal(t, 0, len(cb.frames))
	require.True(t, cb.IsFull())

	// Since the channel output is full, the next call to AddBlock
	// should return the channel out full error
	require.ErrorIs(t, addMiniBlock(cb), derive.CompressorFullErr)
}

// ChannelBuilder_Reset tests the [Reset] function
func ChannelBuilder_Reset(t *testing.T, batchType uint) {
	channelConfig := defaultTestChannelConfig
	channelConfig.BatchType = batchType

	// Lower the max frame size so that we can batch
	channelConfig.MaxFrameSize = 24
	channelConfig.CompressorConfig.TargetNumFrames = 1
	channelConfig.CompressorConfig.TargetFrameSize = 24
	channelConfig.CompressorConfig.ApproxComprRatio = 1

	cb, err := newChannelBuilder(channelConfig, &defaultTestRollupConfig)
	require.NoError(t, err)

	// Add a nonsense block to the channel builder
	require.NoError(t, addMiniBlock(cb))
	require.NoError(t, cb.co.Flush())

	// Check the fields reset in the Reset function
	require.Equal(t, 1, len(cb.blocks))
	require.Equal(t, 0, len(cb.frames))
	// Timeout should be updated in the AddBlock internal call to `updateSwTimeout`
	timeout := uint64(100) + cb.cfg.SeqWindowSize - cb.cfg.SubSafetyMargin
	require.Equal(t, timeout, cb.timeout)
	require.Error(t, cb.fullErr)

	// Output frames so we can set the channel builder frames
	require.NoError(t, cb.OutputFrames())

	// Check the fields reset in the Reset function
	require.Equal(t, 1, len(cb.blocks))
	require.Equal(t, timeout, cb.timeout)
	require.Error(t, cb.fullErr)
	require.Greater(t, len(cb.frames), 1)

	// Reset the channel builder
	require.NoError(t, cb.Reset())

	// Check the fields reset in the Reset function
	require.Equal(t, 0, len(cb.blocks))
	require.Equal(t, 0, len(cb.frames))
	require.Equal(t, uint64(0), cb.timeout)
	require.NoError(t, cb.fullErr)
	require.Equal(t, 0, cb.co.InputBytes())
	require.Equal(t, 0, cb.co.ReadyBytes())
}

// TestBuilderRegisterL1Block tests the RegisterL1Block function
func TestBuilderRegisterL1Block(t *testing.T) {
	channelConfig := defaultTestChannelConfig

	// Construct the channel builder
	cb, err := newChannelBuilder(channelConfig, nil)
	require.NoError(t, err)

	// Assert params modified in RegisterL1Block
	require.Equal(t, uint64(1), channelConfig.MaxChannelDuration)
	require.Equal(t, uint64(0), cb.timeout)

	// Register a new L1 block
	cb.RegisterL1Block(uint64(100))

	// Assert params modified in RegisterL1Block
	require.Equal(t, uint64(1), channelConfig.MaxChannelDuration)
	require.Equal(t, uint64(101), cb.timeout)
}

// TestBuilderRegisterL1BlockZeroMaxChannelDuration tests the RegisterL1Block function
func TestBuilderRegisterL1BlockZeroMaxChannelDuration(t *testing.T) {
	channelConfig := defaultTestChannelConfig

	// Set the max channel duration to 0
	channelConfig.MaxChannelDuration = 0

	// Construct the channel builder
	cb, err := newChannelBuilder(channelConfig, nil)
	require.NoError(t, err)

	// Assert params modified in RegisterL1Block
	require.Equal(t, uint64(0), channelConfig.MaxChannelDuration)
	require.Equal(t, uint64(0), cb.timeout)

	// Register a new L1 block
	cb.RegisterL1Block(uint64(100))

	// Since the max channel duration is set to 0,
	// the L1 block register should not update the timeout
	require.Equal(t, uint64(0), channelConfig.MaxChannelDuration)
	require.Equal(t, uint64(0), cb.timeout)
}

// TestFramePublished tests the FramePublished function
func TestFramePublished(t *testing.T) {
	channelConfig := defaultTestChannelConfig

	// Construct the channel builder
	cb, err := newChannelBuilder(channelConfig, nil)
	require.NoError(t, err)

	// Let's say the block number is fed in as 100
	// and the channel timeout is 1000
	l1BlockNum := uint64(100)
	cb.cfg.ChannelTimeout = uint64(1000)
	cb.cfg.SubSafetyMargin = 100

	// Then the frame published will update the timeout
	cb.FramePublished(l1BlockNum)

	// Now the timeout will be 1000
	require.Equal(t, uint64(1000), cb.timeout)
}

func ChannelBuilder_PendingFrames_TotalFrames(t *testing.T, batchType uint) {
	const tnf = 9
	rng := rand.New(rand.NewSource(94572314))
	require := require.New(t)
	cfg := defaultTestChannelConfig
	cfg.CompressorConfig.TargetFrameSize = 1000
	cfg.MaxFrameSize = 1000
	cfg.CompressorConfig.TargetNumFrames = tnf
	cfg.CompressorConfig.Kind = "shadow"
	cfg.BatchType = batchType
	cb, err := newChannelBuilder(cfg, &defaultTestRollupConfig)
	require.NoError(err)

	// initial builder should be empty
	require.Zero(cb.PendingFrames())
	require.Zero(cb.TotalFrames())

	// fill up
	for {
		block := dtest.RandomL2BlockWithChainId(rng, 4, defaultTestRollupConfig.L2ChainID)
		_, err := cb.AddBlock(block)
		if cb.IsFull() {
			break
		}
		require.NoError(err)
	}
	require.NoError(cb.OutputFrames())

	nf := cb.TotalFrames()
	// require 1 < nf < tnf
	// (because of compression we won't necessarily land exactly at tnf, that's ok)
	require.Greater(nf, 1)
	require.LessOrEqual(nf, tnf)
	require.Equal(nf, cb.PendingFrames())

	// empty queue
	for pf := nf - 1; pf >= 0; pf-- {
		require.True(cb.HasFrame())
		_ = cb.NextFrame()
		require.Equal(cb.PendingFrames(), pf)
		require.Equal(cb.TotalFrames(), nf)
	}
}

func ChannelBuilder_InputBytes(t *testing.T, batchType uint) {
	require := require.New(t)
	rng := rand.New(rand.NewSource(4982432))
	cfg := defaultTestChannelConfig
	cfg.BatchType = batchType
	var spanBatchBuilder *derive.SpanBatchBuilder
	if batchType == derive.SpanBatchType {
		chainId := big.NewInt(1234)
		spanBatchBuilder = derive.NewSpanBatchBuilder(uint64(0), chainId)
	}
	cb, err := newChannelBuilder(cfg, &defaultTestRollupConfig)
	require.NoError(err)

	require.Zero(cb.InputBytes())

	var l int
	for i := 0; i < 5; i++ {
		block := dtest.RandomL2BlockWithChainId(rng, rng.Intn(32), defaultTestRollupConfig.L2ChainID)
		if batchType == derive.SingularBatchType {
			l += blockBatchRlpSize(t, block)
		} else {
			singularBatch, l1Info, err := derive.BlockToSingularBatch(block)
			require.NoError(err)
			spanBatchBuilder.AppendSingularBatch(singularBatch, l1Info.SequenceNumber)
			rawSpanBatch, err := spanBatchBuilder.GetRawSpanBatch()
			require.NoError(err)
			batch := derive.NewBatchData(rawSpanBatch)
			var buf bytes.Buffer
			require.NoError(batch.EncodeRLP(&buf))
			l = buf.Len()
		}
		_, err := cb.AddBlock(block)
		require.NoError(err)
		require.Equal(cb.InputBytes(), l)
	}
}

func ChannelBuilder_OutputBytes(t *testing.T, batchType uint) {
	require := require.New(t)
	rng := rand.New(rand.NewSource(9860372))
	cfg := defaultTestChannelConfig
	cfg.CompressorConfig.TargetFrameSize = 1000
	cfg.MaxFrameSize = 1000
	cfg.CompressorConfig.TargetNumFrames = 16
	cfg.CompressorConfig.ApproxComprRatio = 1.0
	cfg.BatchType = batchType
	cb, err := newChannelBuilder(cfg, &defaultTestRollupConfig)
	require.NoError(err, "newChannelBuilder")

	require.Zero(cb.OutputBytes())

	for {
		block := dtest.RandomL2BlockWithChainId(rng, rng.Intn(32), defaultTestRollupConfig.L2ChainID)
		_, err := cb.AddBlock(block)
		if errors.Is(err, derive.CompressorFullErr) {
			break
		}
		require.NoError(err)
	}

	require.NoError(cb.OutputFrames())
	require.True(cb.IsFull())
	require.Greater(cb.PendingFrames(), 1)

	var flen int
	for cb.HasFrame() {
		f := cb.NextFrame()
		flen += len(f.data)
	}

	require.Equal(cb.OutputBytes(), flen)
}

func blockBatchRlpSize(t *testing.T, b *types.Block) int {
	t.Helper()
	singularBatch, _, err := derive.BlockToSingularBatch(b)
	batch := derive.NewBatchData(singularBatch)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, batch.EncodeRLP(&buf), "RLP-encoding batch")
	return buf.Len()
}
