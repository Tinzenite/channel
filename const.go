package channel

import "errors"

/*
Internal errors of channel.
*/
var (
	errLostAddress = errors.New("could not determine address")
	errOffline     = errors.New("address is not online")
)

/*Default string values*/
const (
	illegalAddress = "unknown_address"
)
