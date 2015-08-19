package channel

import (
	"log"
	"os"
)

/*
transfer is the object associated to a transfer.
*/
type transfer struct {
	file *os.File
	size uint64
	done OnDone
}

/*
close can be called to finish the transfer.
*/
func (t *transfer) Close(success bool) {
	err := t.file.Sync()
	if err != nil {
		log.Println("Transfer: file.Sync:", err)
	}
	err = t.file.Close()
	if err != nil {
		log.Println("Transfer: file.Close:", err)
	}
	// execute callback if exists
	if t.done != nil {
		t.done(success)
	}
}
