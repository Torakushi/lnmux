package lnmux

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bottlepay/lnmux/common"
	"github.com/bottlepay/lnmux/lnd"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntypes"
	"go.uber.org/zap"
)

const (
	resolutionQueueSize = 100
)

type interceptor struct {
	lnd        lnd.LndClient
	logger     *zap.SugaredLogger
	pubKey     common.PubKey
	htlcChan   chan *interceptedHtlc
	heightChan chan int
}

func newInterceptor(lnd lnd.LndClient, logger *zap.SugaredLogger,
	htlcChan chan *interceptedHtlc, heightChan chan int) *interceptor {

	pubKey := lnd.PubKey()
	logger = logger.With("node", pubKey)

	return &interceptor{
		lnd:        lnd,
		logger:     logger,
		pubKey:     pubKey,
		htlcChan:   htlcChan,
		heightChan: heightChan,
	}
}

func (i *interceptor) run(ctx context.Context) {
	defer i.logger.Debugw("Exiting interceptor loop")

	for {
		err := i.start(ctx)
		if err == nil || err == context.Canceled {
			return
		}

		i.logger.Infow("Htlc interceptor error",
			"err", err)

		select {
		// Retry delay.
		case <-time.After(time.Second):

		case <-ctx.Done():
			return
		}
	}
}

func (i *interceptor) start(ctx context.Context) error {
	var cancel func()
	ctx, cancel = context.WithCancel(ctx)
	defer cancel()

	send, recv, err := i.lnd.HtlcInterceptor(ctx)
	if err != nil {
		return err
	}

	i.logger.Debugw("Starting htlc interception")

	// Register for block notifications.
	blockChan, blockErrChan, err := i.lnd.RegisterBlockEpochNtfn(ctx)
	if err != nil {
		return err
	}

	// The block stream immediately sends the current block. Read that to
	// set our initial height.
	select {
	case block := <-blockChan:
		i.logger.Debugw("Initial block height", "height", block.Height)
		i.heightChan <- int(block.Height)

	case <-ctx.Done():
		return ctx.Err()
	}

	var (
		errChan   = make(chan error, 1)
		replyChan = make(
			chan *routerrpc.ForwardHtlcInterceptResponse,
			resolutionQueueSize,
		)
	)

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func(ctx context.Context) {
		defer wg.Done()

		err := i.htlcReceiveLoop(ctx, recv, replyChan)
		if err != nil {
			errChan <- err
		}
	}(ctx)

	for {
		select {
		case err := <-errChan:
			return fmt.Errorf("stream error: %w", err)

		case block := <-blockChan:
			select {
			case i.heightChan <- int(block.Height):

			case <-ctx.Done():
				return ctx.Err()
			}

		case rpcResp, ok := <-replyChan:
			if !ok {
				return errors.New("reply channel full")
			}

			err := send(rpcResp)
			if err != nil {
				return fmt.Errorf("cannot send: %w", err)
			}

		case err := <-blockErrChan:
			return fmt.Errorf("block error: %w", err)

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (i *interceptor) htlcReceiveLoop(ctx context.Context,
	recv func() (*routerrpc.ForwardHtlcInterceptRequest, error),
	replyChan chan *routerrpc.ForwardHtlcInterceptResponse) error {

	var replyChanClosed bool

	for {
		htlc, err := recv()
		if err != nil {
			return err
		}

		hash, err := lntypes.MakeHash(htlc.PaymentHash)
		if err != nil {
			return err
		}

		reply := func(resp *interceptedHtlcResponse) error {
			// Don't try to write if the channel is closed. This
			// callback does not need to be thread-safe.
			if replyChanClosed {
				return errors.New("reply channel closed")
			}

			rpcResp := &routerrpc.ForwardHtlcInterceptResponse{
				IncomingCircuitKey: htlc.IncomingCircuitKey,
				Action:             resp.action,
				Preimage:           resp.preimage[:],
				FailureMessage:     resp.failureMessage,
				FailureCode:        resp.failureCode,
			}

			select {
			case replyChan <- rpcResp:
				return nil

			// When the update channel is full, terminate the subscriber
			// to prevent blocking multiplexer.
			default:
				close(replyChan)
				replyChanClosed = true

				return errors.New("reply channel full")
			}
		}

		circuitKey := newCircuitKeyFromRPC(htlc.IncomingCircuitKey)

		select {
		case i.htlcChan <- &interceptedHtlc{
			source:         i.pubKey,
			circuitKey:     circuitKey,
			hash:           hash,
			onionBlob:      htlc.OnionBlob,
			amountMsat:     int64(htlc.OutgoingAmountMsat),
			expiry:         htlc.OutgoingExpiry,
			outgoingChanID: htlc.OutgoingRequestedChanId,
			reply:          reply,
		}:

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
