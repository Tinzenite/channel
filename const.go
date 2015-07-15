package channel

import "errors"

/*
Internal errors of channel.
*/
var (
	errLostAddress = errors.New("could not determine address")
)

/*Default string values*/
const (
	illegalAddress = "unknown_address"
)
