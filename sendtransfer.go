package channel

import "time"

type sendTransfer struct {
	started    bool
	began      time.Time
	fileNumber uint32
}

/*
buildSendTransfer creates a new transfer with primed values. Note that the timeout
runs from the moment this method is called for isStale.
*/
func buildSendTransfer(fileNumber uint32) *sendTransfer {
	return &sendTransfer{
		started:    false,
		began:      time.Now(),
		fileNumber: fileNumber}
}

/*
isStale returns true if the timeout for starting the send transfer has been
reached without the transfer actually beginning.
*/
func (st *sendTransfer) isStale() bool {
	return time.Since(st.began) > sendTimeout && !st.started
}
