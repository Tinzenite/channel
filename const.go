package channel

import "errors"

/*
Internal errors of channel.
*/
var (
	errLostAddress = errors.New("Could not determine address!")
)

/*Default string values*/
const (
	illegalAddress = "unknown_address"
)
