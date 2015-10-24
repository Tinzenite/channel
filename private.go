package channel

import (
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/codedust/go-tox"
)

/*
addressOf given friend number.
*/
func (channel *Channel) addressOf(friendnumber uint32) (string, error) {
	publicKey, err := channel.tox.FriendGetPublickey(friendnumber)
	if err != nil {
		return "", errLostAddress
	}
	return hex.EncodeToString(publicKey), nil
}

/*
friendNumberOf the given address.
*/
func (channel *Channel) friendNumberOf(address string) (uint32, error) {
	publicKey, err := hex.DecodeString(address)
	if err != nil {
		return 0, err
	}
	return channel.tox.FriendByPublicKey(publicKey)
}

/*******************************************************************************
NOTE: ALL BELOW ARE TOX CALLBACKS
*******************************************************************************/

/*
onFriendRequest calls the appropriate callback, wrapping it sanely for our purposes.
*/
func (channel *Channel) onFriendRequest(_ *gotox.Tox, publicKey []byte, message string) {
	if channel.callbacks != nil {
		// strip key of NOSPAM - this is the only instance where it is passed here
		if len(publicKey) > 32 {
			publicKey = publicKey[:32]
		}
		// all real callbacks are run in separate go routines to keep ToxCore none blocking!
		go channel.callbacks.OnFriendRequest(hex.EncodeToString(publicKey), message)
	} else {
		log.Println(tag, "No callback for OnNewConnection registered!")
	}
}

/*
onFriendMessage calls the appropriate callback, wrapping it sanely for our purposes.
*/
func (channel *Channel) onFriendMessage(_ *gotox.Tox, friendnumber uint32, messagetype gotox.ToxMessageType, message string) {
	/*TODO make sensible*/
	if messagetype == gotox.TOX_MESSAGE_TYPE_NORMAL {
		if channel.callbacks != nil {
			address, err := channel.addressOf(friendnumber)
			if err != nil {
				log.Println(tag, err)
				address = illegalAddress
			}
			// all real callbacks are run in separate go routines to keep ToxCore none blocking!
			go channel.callbacks.OnMessage(address, message)
		} else {
			log.Println(tag, "No callback for OnMessage registered!")
		}
	} else {
		log.Println(tag, "Invalid message type, ignoring!")
	}
}

/*
onFriendConnectionStatusChanges is called when a friend comes online, goes
offline, or the connection state changes. In all cases we terminate any ongoing
transfers (will need to be restarted).
*/
func (channel *Channel) onFriendConnectionStatusChanges(_ *gotox.Tox, friendnumber uint32, connectionstatus gotox.ToxConnection) {
	if channel.log {
		log.Println(tag, "detected status change")
	}
	// get address of friend since we can't execute callbacks without out
	address, err := channel.addressOf(friendnumber)
	if err != nil {
		log.Println(tag, "OnConnected: failed to retrieve address:", err)
		// but continue with default value
	}
	// cancel any running file transfers no matter what changed (if newly connected a disconnect happened before)
	var canceled []uint32
	for filenumber, trans := range channel.transfers {
		if trans.friend == friendnumber {
			trans.Close(false)
			canceled = append(canceled, filenumber)
			// also callback OnFileCanceled!
			if channel.callbacks != nil {
				go channel.callbacks.OnFileCanceled(address, trans.path)
			} else {
				log.Println(tag, "No callback for OnFileCanceled registered!")
			}
		}
	}
	for _, filenumber := range canceled {
		delete(channel.transfers, filenumber)
	}
	// if going offline do nothing
	if connectionstatus == gotox.TOX_CONNECTION_NONE {
		// TODO add callback: OnDisconnected
		return
	}
	if channel.callbacks != nil {
		// all real callbacks are run in separate go routines to keep ToxCore none blocking!
		go channel.callbacks.OnConnected(address)
	} else {
		log.Println(tag, "No callback for OnConnected registered!")
	}
}

/*
onFileRecvControl is called when a file control packet is received.
*/
func (channel *Channel) onFileRecvControl(_ *gotox.Tox, friendnumber uint32, filenumber uint32, fileControl gotox.ToxFileControl) {
	// we only explicitely need to handle cancel because we then have to remove resources
	if fileControl == gotox.TOX_FILE_CONTROL_CANCEL {
		trans, exists := channel.transfers[filenumber]
		if !exists {
			log.Println(tag, "Transfer wasn't even tracked, ignoring!", filenumber)
			// if it doesn't exist, ignore!
			return
		}
		// free resources
		trans.Close(false)
		delete(channel.transfers, filenumber)
		// call callback
		if channel.callbacks != nil {
			address, err := channel.addressOf(friendnumber)
			if err != nil {
				log.Println(tag, "OnFileCanceled:", err)
				return
			}
			// all real callbacks are run in separate go routines to keep ToxCore none blocking!
			go channel.callbacks.OnFileCanceled(address, trans.path)
		} else {
			log.Println(tag, "No callback for OnFileCanceled registered!")
		}
	}
}

/*
onFileRecv is called when a file transfer is to be opened. Will prepare the file
for reception of chunks.
*/
func (channel *Channel) onFileRecv(_ *gotox.Tox, friendnumber uint32, fileNumber uint32, kind gotox.ToxFileKind, filesize uint64, filename string) {
	// we're not interested in avatars
	if kind != gotox.TOX_FILE_KIND_DATA {
		log.Println(tag, "Ignoring non data file transfer!")
		// send cancel so that the other client knows that we blocked it
		channel.tox.FileControl(friendnumber, fileNumber, gotox.TOX_FILE_CONTROL_CANCEL)
		return
	}
	// this requires callbacks to be registered
	if channel.callbacks == nil {
		// required for receiving files
		log.Println(tag, "No callback for OnAllowFile registered!")
		return
	}
	// address
	address, err := channel.addressOf(friendnumber)
	if err != nil {
		log.Println(tag, err.Error())
		address = illegalAddress
	}
	// use callback to check whether to accept from Tinzenite NOTE: this one blocks... :(
	accept, path := channel.callbacks.OnAllowFile(address, filename)
	if !accept {
		return
	}
	// create file at correct location
	/*TODO how are pause & resume handled? FIXME*/
	f, err := os.Create(path)
	if err != nil {
		log.Println(tag, "Creating file to write receival of data to failed!", err)
	}
	// create transfer object
	channel.transfers[fileNumber] = createTransfer(path, friendnumber, f, filesize, func(done bool) {
		if !done {
			log.Println("Transfer: sending failed!", path)
		}
	})
	// accept file send request if we come to here
	channel.tox.FileControl(friendnumber, fileNumber, gotox.TOX_FILE_CONTROL_RESUME)
}

/*
onFileRecvChunk is called when a chunk of a file is received. Writes the data to
the correct file.
*/
func (channel *Channel) onFileRecvChunk(_ *gotox.Tox, friendnumber uint32, fileNumber uint32, position uint64, data []byte) {
	tran, exists := channel.transfers[fileNumber]
	if !exists {
		// ignore zero length chunk that is sent to signal a complete transfer
		if len(data) == 0 {
			return
		}
		log.Println(tag, "Receive transfer doesn't seem to exist!", fileNumber)
		// send that we won't be accepting this transfer after all
		channel.tox.FileControl(friendnumber, fileNumber, gotox.TOX_FILE_CONTROL_CANCEL)
		// and we're done
		return
	}
	// write date to disk
	tran.file.WriteAt(data, (int64)(position))
	// update progress
	tran.SetProgress(position + uint64(len(data)))
	// this means the file has been completey received
	if position+uint64(len(data)) >= tran.size {
		pathelements := strings.Split(tran.file.Name(), "/")
		// callback with file name / identification
		address, _ := channel.addressOf(friendnumber)
		name := pathelements[len(pathelements)-1]
		path := strings.Join(pathelements, "/")
		// finish transfer
		tran.Close(true)
		delete(channel.transfers, fileNumber)
		// call callback
		if channel.callbacks != nil {
			// all real callbacks are run in separate go routines to keep ToxCore none blocking!
			go channel.callbacks.OnFileReceived(address, path, name)
		} else {
			// this shouldn't happen as file can only be received with callbacks, but let us be sure
			log.Println(tag, "No callback for OnFileReceived registered!")
		}
	}
}

/*
onFileChunkRequest is called when a chunk must be sent.
*/
func (channel *Channel) onFileChunkRequest(_ *gotox.Tox, friendNumber uint32, fileNumber uint32, position uint64, length uint64) {
	trans, exists := channel.transfers[fileNumber]
	// sanity check
	if !exists {
		log.Println(tag, "Send transfer doesn't seem to exist!", fileNumber)
		return
	}
	// ensure that length is valid
	if length+position > trans.size {
		length = trans.size - position
	}
	// if we're already done we finish here without sending any more chunks
	if length == 0 {
		trans.Close(true)
		delete(channel.transfers, fileNumber)
		return
	}
	// get bytes to send
	data := make([]byte, length)
	_, err := trans.file.ReadAt(data, int64(position))
	if err != nil {
		fmt.Println(tag, "Error reading file:", err)
		return
	}
	// send
	err = channel.tox.FileSendChunk(friendNumber, fileNumber, position, data)
	if err != nil {
		log.Println(tag, "File send error: ", err)
	}
	// update progress
	trans.SetProgress(position + length)
}
