package channel

import "errors"

/*
Internal errors of channel.
*/
var (
	errLostAddress      = errors.New("could not determine address")
	errOffline          = errors.New("address is not online")
	errBootstrap        = errors.New("failed to bootstrap to any given node")
	errTransferNotFound = errors.New("could not determine transfer for file name")
	errSendBufferFull   = errors.New("sending buffer is full")
)

/*Default string values*/
const (
	illegalAddress = "unknown_address"
	tag            = "Channel:"
)

/*
State is an enumeration for notifying callbacks of transfer states.
*/
type State int

const (
	/*StNone is the default value.*/
	StNone State = iota
	/*StSuccess is used to signal a successful transfer.*/
	StSuccess
	/*StFailed is used to signal a failed transfer.*/
	StFailed
	/*StCanceled means that a transfer was canceled.*/
	StCanceled
	/*StTimeout means the transfer timed out and was canceled.*/
	StTimeout
)

func (s State) String() string {
	switch s {
	case StNone:
		return "none"
	case StSuccess:
		return "success"
	case StFailed:
		return "failed"
	case StCanceled:
		return "canceled"
	case StTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}
