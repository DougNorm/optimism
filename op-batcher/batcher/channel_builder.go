package batcher

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/DougNorm/optimism/op-batcher/compressor"
	"github.com/DougNorm/optimism/op-node/rollup"
	"github.com/DougNorm/optimism/op-node/rollup/derive"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	ErrInvalidChannelTimeout = errors.New("channel timeout is less than the safety margin")
	ErrMaxFrameIndex         = errors.New("max frame index reached (uint16)")
	ErrMaxDurationReached    = errors.New("max channel duration reached")
	ErrChannelTimeoutClose   = errors.New("close to channel timeout")
	ErrSeqWindowClose        = errors.New("close to sequencer window timeout")
	ErrTerminated            = errors.New("channel terminated")
)

type ChannelFullError struct {
	Err error
}

func (e *ChannelFullError) Error() string {
	return "channel full: " + e.Err.Error()
}

func (e *ChannelFullError) Unwrap() error {
	return e.Err
}

type ChannelConfig struct {
	// Number of epochs (L1 blocks) per sequencing window, including the epoch
	// L1 origin block itself
	SeqWindowSize uint64
	// The maximum number of L1 blocks that the inclusion transactions of a
	// channel's frames can span.
	ChannelTimeout uint64

	// Builder Config

	// MaxChannelDuration is the maximum duration (in #L1-blocks) to keep the
	// channel open. This allows control over how long a channel is kept open
	// during times of low transaction volume.
	//
	// If 0, duration checks are disabled.
	MaxChannelDuration uint64
	// The batcher tx submission safety margin (in #L1-blocks) to subtract from
	// a channel's timeout and sequencing window, to guarantee safe inclusion of
	// a channel on L1.
	SubSafetyMargin uint64
	// The maximum byte-size a frame can have.
	MaxFrameSize uint64

	// CompressorConfig contains the configuration for creating new compressors.
	CompressorConfig compressor.Config

	// BatchType indicates whether the channel uses SingularBatch or SpanBatch.
	BatchType uint
}

// Check validates the [ChannelConfig] parameters.
func (cc *ChannelConfig) Check() error {
	// The [ChannelTimeout] must be larger than the [SubSafetyMargin].
	// Otherwise, new blocks would always be considered timed out.
	if cc.ChannelTimeout < cc.SubSafetyMargin {
		return ErrInvalidChannelTimeout
	}

	// If the [MaxFrameSize] is set to 0, the channel builder
	// will infinitely loop when trying to create frames in the
	// [channelBuilder.OutputFrames] function.
	if cc.MaxFrameSize == 0 {
		return errors.New("max frame size cannot be zero")
	}

	// If the [MaxFrameSize] is less than [FrameV0OverHeadSize], the channel
	// out will underflow the maxSize variable in the [derive.ChannelOut].
	// Since it is of type uint64, it will wrap around to a very large
	// number, making the frame size extremely large.
	if cc.MaxFrameSize < derive.FrameV0OverHeadSize {
		return fmt.Errorf("max frame size %d is less than the minimum 23", cc.MaxFrameSize)
	}

	if cc.BatchType > derive.SpanBatchType {
		return fmt.Errorf("unrecognized batch type: %d", cc.BatchType)
	}

	return nil
}

type frameID struct {
	chID        derive.ChannelID
	frameNumber uint16
}

type frameData struct {
	data []byte
	id   frameID
}

// channelBuilder uses a ChannelOut to create a channel with output frame
// size approximation.
type channelBuilder struct {
	cfg ChannelConfig

	// L1 block number timeout of combined
	// - channel duration timeout,
	// - consensus channel timeout,
	// - sequencing window timeout.
	// 0 if no block number timeout set yet.
	timeout uint64
	// reason for currently set timeout
	timeoutReason error

	// Reason for the channel being full. Set by setFullErr so it's always
	// guaranteed to be a ChannelFullError wrapping the specific reason.
	fullErr error
	// current channel
	co derive.ChannelOut
	// list of blocks in the channel. Saved in case the channel must be rebuilt
	blocks []*types.Block
	// frames data queue, to be send as txs
	frames []frameData
	// total frames counter
	numFrames int
	// total amount of output data of all frames created yet
	outputBytes int
}

// newChannelBuilder creates a new channel builder or returns an error if the
// channel out could not be created.
func newChannelBuilder(cfg ChannelConfig, rcfg *rollup.Config) (*channelBuilder, error) {
	c, err := cfg.CompressorConfig.NewCompressor()
	if err != nil {
		return nil, err
	}
	var spanBatchBuilder *derive.SpanBatchBuilder
	if cfg.BatchType == derive.SpanBatchType {
		spanBatchBuilder = derive.NewSpanBatchBuilder(rcfg.Genesis.L2Time, rcfg.L2ChainID)
	}
	co, err := derive.NewChannelOut(cfg.BatchType, c, spanBatchBuilder)
	if err != nil {
		return nil, err
	}

	return &channelBuilder{
		cfg: cfg,
		co:  co,
	}, nil
}

func (c *channelBuilder) ID() derive.ChannelID {
	return c.co.ID()
}

// InputBytes returns the total amount of input bytes added to the channel.
func (c *channelBuilder) InputBytes() int {
	return c.co.InputBytes()
}

// ReadyBytes returns the amount of bytes ready in the compression pipeline to
// output into a frame.
func (c *channelBuilder) ReadyBytes() int {
	return c.co.ReadyBytes()
}

func (c *channelBuilder) OutputBytes() int {
	return c.outputBytes
}

// Blocks returns a backup list of all blocks that were added to the channel. It
// can be used in case the channel needs to be rebuilt.
func (c *channelBuilder) Blocks() []*types.Block {
	return c.blocks
}

// Reset resets the internal state of the channel builder so that it can be
// reused. Note that a new channel id is also generated by Reset.
func (c *channelBuilder) Reset() error {
	c.blocks = c.blocks[:0]
	c.frames = c.frames[:0]
	c.timeout = 0
	c.fullErr = nil
	return c.co.Reset()
}

// AddBlock adds a block to the channel compression pipeline. IsFull should be
// called afterwards to test whether the channel is full. If full, a new channel
// must be started.
//
// AddBlock returns a ChannelFullError if called even though the channel is
// already full. See description of FullErr for details.
//
// AddBlock also returns the L1BlockInfo that got extracted from the block's
// first transaction for subsequent use by the caller.
//
// Call OutputFrames() afterwards to create frames.
func (c *channelBuilder) AddBlock(block *types.Block) (derive.L1BlockInfo, error) {
	if c.IsFull() {
		return derive.L1BlockInfo{}, c.FullErr()
	}

	batch, l1info, err := derive.BlockToSingularBatch(block)
	if err != nil {
		return l1info, fmt.Errorf("converting block to batch: %w", err)
	}

	if _, err = c.co.AddSingularBatch(batch, l1info.SequenceNumber); errors.Is(err, derive.ErrTooManyRLPBytes) || errors.Is(err, derive.CompressorFullErr) {
		c.setFullErr(err)
		return l1info, c.FullErr()
	} else if err != nil {
		return l1info, fmt.Errorf("adding block to channel out: %w", err)
	}
	c.blocks = append(c.blocks, block)
	c.updateSwTimeout(batch)

	if err = c.co.FullErr(); err != nil {
		c.setFullErr(err)
		// Adding this block still worked, so don't return error, just mark as full
	}

	return l1info, nil
}

// Timeout management

// RegisterL1Block should be called whenever a new L1-block is seen.
//
// It ensures proper tracking of all possible timeouts (max channel duration,
// close to consensus channel timeout, close to end of sequencing window).
func (c *channelBuilder) RegisterL1Block(l1BlockNum uint64) {
	c.updateDurationTimeout(l1BlockNum)
	c.checkTimeout(l1BlockNum)
}

// FramePublished should be called whenever a frame of this channel got
// published with the L1-block number of the block that the frame got included
// in.
func (c *channelBuilder) FramePublished(l1BlockNum uint64) {
	timeout := l1BlockNum + c.cfg.ChannelTimeout - c.cfg.SubSafetyMargin
	c.updateTimeout(timeout, ErrChannelTimeoutClose)
}

// updateDurationTimeout updates the block timeout with the channel duration
// timeout derived from the given L1-block number. The timeout is only moved
// forward if the derived timeout is earlier than the currently set timeout.
//
// It does nothing if the max channel duration is set to 0.
func (c *channelBuilder) updateDurationTimeout(l1BlockNum uint64) {
	if c.cfg.MaxChannelDuration == 0 {
		return
	}
	timeout := l1BlockNum + c.cfg.MaxChannelDuration
	c.updateTimeout(timeout, ErrMaxDurationReached)
}

// updateSwTimeout updates the block timeout with the sequencer window timeout
// derived from the batch's origin L1 block. The timeout is only moved forward
// if the derived sequencer window timeout is earlier than the currently set
// timeout.
func (c *channelBuilder) updateSwTimeout(batch *derive.SingularBatch) {
	timeout := uint64(batch.EpochNum) + c.cfg.SeqWindowSize - c.cfg.SubSafetyMargin
	c.updateTimeout(timeout, ErrSeqWindowClose)
}

// updateTimeout updates the timeout block to the given block number if it is
// earlier than the current block timeout, or if it still unset.
//
// If the timeout is updated, the provided reason will be set as the channel
// full error reason in case the timeout is hit in the future.
func (c *channelBuilder) updateTimeout(timeoutBlockNum uint64, reason error) {
	if c.timeout == 0 || c.timeout > timeoutBlockNum {
		c.timeout = timeoutBlockNum
		c.timeoutReason = reason
	}
}

// checkTimeout checks if the channel is timed out at the given block number and
// in this case marks the channel as full, if it wasn't full already.
func (c *channelBuilder) checkTimeout(blockNum uint64) {
	if !c.IsFull() && c.TimedOut(blockNum) {
		c.setFullErr(c.timeoutReason)
	}
}

// TimedOut returns whether the passed block number is after the timeout block
// number. If no block timeout is set yet, it returns false.
func (c *channelBuilder) TimedOut(blockNum uint64) bool {
	return c.timeout != 0 && blockNum >= c.timeout
}

// IsFull returns whether the channel is full.
// FullErr returns the reason for the channel being full.
func (c *channelBuilder) IsFull() bool {
	return c.fullErr != nil
}

// FullErr returns the reason why the channel is full. If not full yet, it
// returns nil.
//
// It returns a ChannelFullError wrapping one of the following possible reasons
// for the channel being full:
//   - derive.CompressorFullErr if the compressor target has been reached,
//   - derive.MaxRLPBytesPerChannel if the general maximum amount of input data
//     would have been exceeded by the latest AddBlock call,
//   - ErrMaxFrameIndex if the maximum number of frames has been generated
//     (uint16),
//   - ErrMaxDurationReached if the max channel duration got reached,
//   - ErrChannelTimeoutClose if the consensus channel timeout got too close,
//   - ErrSeqWindowClose if the end of the sequencer window got too close,
//   - ErrTerminated if the channel was explicitly terminated.
func (c *channelBuilder) FullErr() error {
	return c.fullErr
}

func (c *channelBuilder) setFullErr(err error) {
	c.fullErr = &ChannelFullError{Err: err}
}

// OutputFrames creates new frames with the channel out. It should be called
// after AddBlock and before iterating over available frames with HasFrame and
// NextFrame.
//
// If the channel isn't full yet, it will conservatively only
// pull readily available frames from the compression output.
// If it is full, the channel is closed and all remaining
// frames will be created, possibly with a small leftover frame.
func (c *channelBuilder) OutputFrames() error {
	if c.IsFull() {
		return c.closeAndOutputAllFrames()
	}
	return c.outputReadyFrames()
}

// outputReadyFrames creates new frames as long as there's enough data ready in
// the channel out compression pipeline.
//
// This is part of an optimization to already generate frames and send them off
// as txs while still collecting blocks in the channel builder.
func (c *channelBuilder) outputReadyFrames() error {
	// TODO: Decide whether we want to fill frames to max size and use target
	// only for estimation, or use target size.
	for c.co.ReadyBytes() >= int(c.cfg.MaxFrameSize) {
		if err := c.outputFrame(); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (c *channelBuilder) closeAndOutputAllFrames() error {
	if err := c.co.Close(); err != nil {
		return fmt.Errorf("closing channel out: %w", err)
	}

	for {
		if err := c.outputFrame(); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
	}
}

// outputFrame creates one new frame and adds it to the frames queue.
// Note that compressed output data must be available on the underlying
// ChannelOut, or an empty frame will be produced.
func (c *channelBuilder) outputFrame() error {
	var buf bytes.Buffer
	fn, err := c.co.OutputFrame(&buf, c.cfg.MaxFrameSize)
	if err != io.EOF && err != nil {
		return fmt.Errorf("writing frame[%d]: %w", fn, err)
	}

	// Mark as full if max index reached
	// TODO: If there's still data in the compression pipeline of the channel out,
	// we would miss it and the whole channel would be broken because the last
	// frames would never be generated...
	// Hitting the max index is impossible with current parameters, so ignore for
	// now. Note that in order to properly catch this, we'd need to call Flush
	// after every block addition to estimate how many more frames are coming.
	if fn == math.MaxUint16 {
		c.setFullErr(ErrMaxFrameIndex)
	}

	frame := frameData{
		id:   frameID{chID: c.co.ID(), frameNumber: fn},
		data: buf.Bytes(),
	}
	c.frames = append(c.frames, frame)
	c.numFrames++
	c.outputBytes += len(frame.data)
	return err // possibly io.EOF (last frame)
}

// Close immediately marks the channel as full with an ErrTerminated
// if the channel is not already full.
func (c *channelBuilder) Close() {
	if !c.IsFull() {
		c.setFullErr(ErrTerminated)
	}
}

// TotalFrames returns the total number of frames that were created in this channel so far.
// It does not decrease when the frames queue is being emptied.
func (c *channelBuilder) TotalFrames() int {
	return c.numFrames
}

// HasFrame returns whether there's any available frame. If true, it can be
// popped using NextFrame().
//
// Call OutputFrames before to create new frames from the channel out
// compression pipeline.
func (c *channelBuilder) HasFrame() bool {
	return len(c.frames) > 0
}

// PendingFrames returns the number of pending frames in the frames queue.
// It is larger zero iff HasFrames() returns true.
func (c *channelBuilder) PendingFrames() int {
	return len(c.frames)
}

// NextFrame returns the next available frame.
// HasFrame must be called prior to check if there's a next frame available.
// Panics if called when there's no next frame.
func (c *channelBuilder) NextFrame() frameData {
	if len(c.frames) == 0 {
		panic("no next frame")
	}

	f := c.frames[0]
	c.frames = c.frames[1:]
	return f
}

// PushFrame adds the frame back to the internal frames queue. Panics if not of
// the same channel.
func (c *channelBuilder) PushFrame(frame frameData) {
	if frame.id.chID != c.ID() {
		panic("wrong channel")
	}
	c.frames = append(c.frames, frame)
}
