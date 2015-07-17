package channel

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/codedust/go-tox"
	"github.com/xamino/tox-dynboot"
)

/*
Channel is a wrapper of the gotox wrapper that creates and manages the underlying Tox
instance.

TODO all callbacks will block, need to avoid that especially when user interaction is required
*/
type Channel struct {
	tox                *gotox.Tox
	callbacks          Callbacks
	wg                 sync.WaitGroup
	stop               chan bool
	transfers          map[uint32]*os.File
	transfersFilesizes map[uint32]uint64
}

/*
Create and starts a new tox channel that continously runs in the background
until this object is destroyed.
*/
func Create(name string, toxdata []byte, callbacks Callbacks) (*Channel, error) {
	if name == "" {
		return nil, errors.New("CreateChannel called with no name!")
	}
	var init bool
	var channel = &Channel{}
	var options *gotox.Options
	var err error

	// prepare for file transfers
	channel.transfers = make(map[uint32]*os.File)
	channel.transfersFilesizes = make(map[uint32]uint64)

	// this decides whether we are initiating a new connection or using an existing one
	if toxdata == nil {
		options = &gotox.Options{
			true, true,
			gotox.TOX_PROXY_TYPE_NONE, "127.0.0.1", 5555, 0, 0, 0,
			gotox.TOX_SAVEDATA_TYPE_NONE, nil}
		init = true
	} else {
		options = &gotox.Options{
			true, true,
			gotox.TOX_PROXY_TYPE_NONE, "127.0.0.1", 5555, 0, 0, 0,
			gotox.TOX_SAVEDATA_TYPE_TOX_SAVE, toxdata}
		init = false
	}
	channel.tox, err = gotox.New(options)
	if err != nil {
		return nil, err
	}
	if init {
		channel.tox.SelfSetName(name)
		channel.tox.SelfSetStatusMessage("Tin Peer")
	}
	err = channel.tox.SelfSetStatus(gotox.TOX_USERSTATUS_NONE)
	// Register our callbacks
	channel.tox.CallbackFriendRequest(channel.onFriendRequest)
	channel.tox.CallbackFriendMessage(channel.onFriendMessage)
	channel.tox.CallbackFileRecvControl(channel.onFileRecvControl)
	channel.tox.CallbackFileRecv(channel.onFileRecv)
	channel.tox.CallbackFileRecvChunk(channel.onFileRecvChunk)
	channel.tox.CallbackFileChunkRequest(channel.onFileChunkRequest)
	// some things must only be done if first start
	if init {
		// Bootstrap
		toxNode, err := toxdynboot.FetchFirstAlive(200 * time.Millisecond)
		if err != nil {
			return nil, err
		}
		err = channel.tox.Bootstrap(toxNode.IPv4, toxNode.Port, toxNode.PublicKey)
		if err != nil {
			return nil, err
		}
	}
	// register callbacks
	channel.callbacks = callbacks
	// now to run it:
	channel.wg.Add(1)
	channel.stop = make(chan bool, 1)
	go channel.run()
	return channel, nil
}

// --- public methods here ---

/*
Close shuts down the channel.
*/
func (channel *Channel) Close() {
	// send stop signal
	channel.stop <- false
	// wait for it to close
	channel.wg.Wait()
	// kill tox
	channel.tox.Kill()
}

/*
Address of the Tox instance.
*/
func (channel *Channel) Address() (string, error) {
	address, err := channel.tox.SelfGetAddress()
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(address)), nil
}

/*
ToxData returns the underlying current representation of the tox data. Can be
used to store a Tox instance to disk.
*/
func (channel *Channel) ToxData() ([]byte, error) {
	return channel.tox.GetSavedata()
}

/*
Send a message to the given peer address.
*/
func (channel *Channel) Send(address, message string) error {
	if ok, _ := channel.IsOnline(address); !ok {
		return errOffline
	}
	// find friend id to send to
	key, err := hex.DecodeString(address)
	if err != nil {
		return err
	}
	id, err := channel.tox.FriendByPublicKey(key)
	if err != nil {
		return err
	}
	// returns message ID but we currently don't use it
	_, err = channel.tox.FriendSendMessage(id, gotox.TOX_MESSAGE_TYPE_NORMAL, message)
	return err
}

/*
SendFile sends a file to the given address. NOTE: Will block until done!
*/
func (channel *Channel) SendFile(address string, path string, identification string) error {
	if ok, _ := channel.IsOnline(address); !ok {
		return errOffline
	}
	// find friend id to send to
	key, err := hex.DecodeString(address)
	if err != nil {
		return err
	}
	id, err := channel.tox.FriendByPublicKey(key)
	if err != nil {
		return err
	}
	// get file
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	// do NOT close file! must be done elsewhere since we may need it later
	// get file size
	stat, err := os.Lstat(path)
	if err != nil {
		file.Close()
		return err
	}
	size := uint64(stat.Size())
	// prepare send (file will be transmitted via filechunk)
	fileNumber, err := channel.tox.FileSend(id, gotox.TOX_FILE_KIND_DATA, size, nil, identification)
	if err != nil {
		file.Close()
		return err
	}
	channel.transfers[fileNumber] = file
	channel.transfersFilesizes[fileNumber] = size
	return nil
}

/*
AcceptConnection accepts the given address as a connection partner.
*/
func (channel *Channel) AcceptConnection(address string) error {
	publicKey, err := hex.DecodeString(address)
	if err != nil {
		return err
	}
	// ignore friendnumber
	_, err = channel.tox.FriendAddNorequest(publicKey)
	return err
}

/*
RequestConnection sends a friend request to the given address with the sending
peer information as the message for bootstrapping.
*/
func (channel *Channel) RequestConnection(address, message string) error {
	publicKey, err := hex.DecodeString(address)
	if err != nil {
		return err
	}
	// send non blocking friend request
	_, err = channel.tox.FriendAdd(publicKey, message)
	return err
}

/*
IsOnline checks whether the given address is currently reachable.
*/
func (channel *Channel) IsOnline(address string) (bool, error) {
	publicKey, err := hex.DecodeString(address)
	if err != nil {
		return false, err
	}
	num, err := channel.tox.FriendByPublicKey(publicKey)
	if err != nil {
		return false, err
	}
	status, err := channel.tox.FriendGetConnectionStatus(num)
	if err != nil {
		return false, err
	}
	return status != gotox.TOX_CONNECTION_NONE, nil
}

/*
NameOf the key associated to the given address.
*/
func (channel *Channel) NameOf(address string) (string, error) {
	publicKey, err := hex.DecodeString(address)
	if err != nil {
		return "", err
	}
	num, err := channel.tox.FriendByPublicKey(publicKey)
	if err != nil {
		return "", err
	}
	name, err := channel.tox.FriendGetName(num)
	if err != nil {
		return "", err
	}
	return name, nil
}

// --- private methods here ---

/*
run is the background go routine method that keeps the Tox instance iterating
until Close() is called.
*/
func (channel *Channel) run() {
	for {
		temp, _ := channel.tox.IterationInterval()
		intervall := time.Duration(temp) * time.Millisecond
		select {
		case <-channel.stop:
			channel.wg.Done()
			return
		case <-time.Tick(intervall):
			err := channel.tox.Iterate()
			if err != nil {
				// TODO what do we do here? Can we cleanly close the channel and
				// catch the error further up?
				log.Println(err.Error())
			}
		} // select
	} // for
}

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
onFriendRequest calls the appropriate callback, wrapping it sanely for our purposes.
*/
func (channel *Channel) onFriendRequest(_ *gotox.Tox, publicKey []byte, message string) {
	if channel.callbacks != nil {
		channel.callbacks.OnNewConnection(hex.EncodeToString(publicKey), message)
	} else {
		log.Println("Error: callbacks are nil!")
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
				log.Println(err.Error())
				address = illegalAddress
			}
			channel.callbacks.OnMessage(address, message)
		} else {
			log.Println("Error: callbacks are nil!")
		}
	}
}

/*
TODO implement and comment
*/
func (channel *Channel) onFileRecvControl(_ *gotox.Tox, friendnumber uint32, filenumber uint32, fileControl gotox.ToxFileControl) {
	// we only explicitely need to handle cancel because we then have to remove resources
	if fileControl == gotox.TOX_FILE_CONTROL_CANCEL {
		log.Println("Transfer was canceled!")
		// free resources
		channel.transfers[filenumber].Close()
		delete(channel.transfers, filenumber)
		delete(channel.transfersFilesizes, filenumber)
	}
}

/*
TODO implement and comment
*/
func (channel *Channel) onFileRecv(_ *gotox.Tox, friendnumber uint32, filenumber uint32, kind gotox.ToxFileKind, filesize uint64, filename string) {
	address, err := channel.addressOf(friendnumber)
	if err != nil {
		log.Println(err.Error())
		address = illegalAddress
	}
	// use callback to check whether to accept from Tinzenite
	accept, path := channel.callbacks.OnAllowFile(address, filename)
	if !accept {
		return
	}
	// accept file send request if we come to here
	channel.tox.FileControl(friendnumber, true, filenumber, gotox.TOX_FILE_CONTROL_RESUME, nil)
	// create file at correct location
	/*TODO how are pause & resume handled?*/
	f, _ := os.Create(path)
	// Append f to the map[uint8]*os.File
	channel.transfers[filenumber] = f
	channel.transfersFilesizes[filenumber] = filesize
}

/*
onFileRecvChunk is called when a chunk of a file is received. Writes the data to
the correct file.
*/
func (channel *Channel) onFileRecvChunk(_ *gotox.Tox, friendnumber uint32, filenumber uint32, position uint64, data []byte) {
	// Write data to the hopefully valid *File handle
	if f, exists := channel.transfers[filenumber]; exists {
		f.WriteAt(data, (int64)(position))
	} else {
		log.Println("File doesn't seem to exist!")
		return
	}
	// this means the file has been completey received
	if position == channel.transfersFilesizes[filenumber] {
		// ensure file is written
		f := channel.transfers[filenumber]
		err := f.Sync()
		if err != nil {
			log.Println("Disk error: " + err.Error())
			return
		}
		pathelements := strings.Split(f.Name(), "/")
		f.Close()
		// free resources
		delete(channel.transfers, filenumber)
		delete(channel.transfersFilesizes, filenumber)
		// callback with file name / identification
		address, _ := channel.addressOf(friendnumber)
		name := pathelements[len(pathelements)-1]
		path := strings.Join(pathelements, "/")
		channel.callbacks.OnFileReceived(address, path, name)
	}
}

/*
onFileChunkRequest is called when a chunk must be sent.
*/
func (channel *Channel) onFileChunkRequest(_ *gotox.Tox, friendNumber uint32, fileNumber uint32, position uint64, length uint64) {
	size, ok := channel.transfersFilesizes[fileNumber]
	// sanity check
	if !ok {
		log.Println("Failed to read from channel.transfers!")
		return
	}
	// recalculate length if near end of file
	if length+position > size {
		length = size - position
	}
	// get bytes to send
	data := make([]byte, length)
	_, err := channel.transfers[fileNumber].ReadAt(data, int64(position))
	if err != nil {
		fmt.Println("Error reading file: " + err.Error())
		return
	}
	// send
	err = channel.tox.FileSendChunk(friendNumber, fileNumber, position, data)
	if err != nil {
		log.Println("File send error: " + err.Error())
	}
}
