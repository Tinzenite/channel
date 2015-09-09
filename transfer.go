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
	progress     uint64
	doneCallback OnDone
	isDone       bool
}

/*
createTransfer builds a transfer object for the given file and the given callback.
*/
func createTransfer(file *os.File, size uint64, callback OnDone) *transfer {
	return &transfer{
		file:         file,
		size:         size,
		progress:     0,
		doneCallback: callback,
		isDone:       false}
}

func (t *transfer) SetProgress(value uint64) {
	t.progress = value
}

/*
Percentage returns in percent the amount already transfered.
*/
func (t *transfer) Percentage() int {
	// catch zero values
	if t.progress == 0 || t.size == 0 {
		return 0
	}
	// calculate
	return int(100.0 * (float32(t.progress) / float32(t.size)))
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
