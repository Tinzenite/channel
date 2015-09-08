package channel

import (
	"log"
	"os"
)

/*
transfer is the object associated to a transfer.
*/
type transfer struct {
	file         *os.File
	size         uint64
	doneCallback OnDone
	isDone       bool
}

/*
createTransfer builds a transfer object for the given file and the given callback.
*/
func createTransfer(file *os.File, callback OnDone) *transfer {
	stat, err := file.Stat()
	if err != nil {
		return nil
	}
	size := uint64(stat.Size())
	return &transfer{
		file:         file,
		size:         size,
		doneCallback: callback,
		isDone:       false}
}

/*
close can be called to finish the transfer.
*/
func (t *transfer) Close(success bool) {
	if t.isDone {
		log.Println("Transfer: WARNING: already closed! Won't execute.")
		return
	}
	// flag that we're done
	t.isDone = true
	// finish writing file
	err := t.file.Sync()
	if err != nil {
		log.Println("Transfer: file.Sync:", err)
	}
	err = t.file.Close()
	if err != nil {
		log.Println("Transfer: file.Close:", err)
	}
	// execute callback if exists
	if t.doneCallback != nil {
		t.doneCallback(success)
	}
	// and we're done
}
