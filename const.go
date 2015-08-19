package channel

import "errors"

/*
Internal errors of channel.
*/
var (
	errLostAddress = errors.New("could not determine address")
	errOffline     = errors.New("address is not online")
	errBootstrap   = errors.New("failed to bootstrap to any given node")
)

/*Default string values*/
const (
	illegalAddress = "unknown_address"
	tag            = "Channel:"
)
