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
	tox       *gotox.Tox
	callbacks Callbacks
	wg        sync.WaitGroup
	stop      chan bool
	transfers map[uint32]transfer
	log       bool
}

/*
transfer is the object associated to a transfer.
*/
type transfer struct {
	file *os.File
	size uint64
	done OnDone
}

/*
execute the done() if it exists!
*/
func (t *transfer) execute(success bool) {
	if t.done != nil {
		t.done(success)
	}
}

/*
OnDone is the function that is executed once the file has been sent / received.
Can be nil.
*/
type OnDone func(success bool)

/*
Create and starts a new tox channel that continously runs in the background
until this object is destroyed.
*/
func Create(name string, toxdata []byte, callbacks Callbacks) (*Channel, error) {
	// other than name everyhting may be nil
	if name == "" {
		return nil, errors.New("CreateChannel called with no name!")
	}
	var init bool
	var channel = &Channel{}
	var options *gotox.Options
	var err error

	/*TODO remove, is only temp*/
	channel.log = false

	// prepare for file transfers
	channel.transfers = make(map[uint32]transfer)

	// this decides whether we are initiating a new connection or using an existing one
	if toxdata == nil {
		log.Println("Channel:", "WARNING create called with empty ToxData.")
		// updated from gotox: nil options okay on first init
		options = nil
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
		channel.tox.SelfSetStatusMessage("Tinzenite Peer")
	}
	err = channel.tox.SelfSetStatus(gotox.TOX_USERSTATUS_NONE)
	// Register our callbacks
	channel.tox.CallbackFriendRequest(channel.onFriendRequest)
	channel.tox.CallbackFriendMessage(channel.onFriendMessage)
	channel.tox.CallbackFriendConnectionStatusChanges(channel.onFriendConnectionStatusChanges)
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
	if channel.log {
		log.Println("Channel: created.")
	}
	return channel, nil
}

// --- public methods here ---

/*
Close shuts down the channel.
*/
func (channel *Channel) Close() {
	// TODO DEBUG NOTE DEBUG
	log.Println("DEBUG: TODO debug why won't close! Implement timeout?")
	// send stop signal
	channel.stop <- true
	// wait for it to close
	channel.wg.Wait()
	// kill tox
	channel.tox.Kill()
	// clean all file transfers
	for _, transfer := range channel.transfers {
		transfer.execute(false)
		transfer.file.Close()
	}
	if channel.log {
		log.Println("Channel: closed.")
	}
}

/*
ConnectionAddress of the Tox instance. This is the address that can be used to
send friend requests to.
*/
func (channel *Channel) ConnectionAddress() (string, error) {
	address, err := channel.tox.SelfGetAddress()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(address), nil
}

/*
Address of the Tox instance.
*/
func (channel *Channel) Address() (string, error) {
	address, err := channel.tox.SelfGetAddress()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(address)[:64], nil
}

/*
OnlineAddresses returns a list of all addresses currently online.
*/
func (channel *Channel) OnlineAddresses() ([]string, error) {
	var onlineAddresses []string
	addresses, err := channel.FriendAddresses()
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		online, err := channel.IsOnline(address)
		if err != nil {
			return nil, err
		}
		if online {
			onlineAddresses = append(onlineAddresses, address)
		}
	}
	return onlineAddresses, nil
}

/*
FriendAddresses returns a list of addresses of all friends.
*/
func (channel *Channel) FriendAddresses() ([]string, error) {
	friends, err := channel.tox.SelfGetFriendlist()
	if err != nil {
		return nil, err
	}
	var addresses []string
	for _, friend := range friends {
		address, err := channel.tox.FriendGetPublickey(friend)
		if err != nil {
			return nil, err
		}
		addresses = append(addresses, hex.EncodeToString(address))
	}
	return addresses, nil
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
	if ok, err := channel.IsOnline(address); !ok {
		if err != nil {
			return err
		}
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
	/*
		if channel.log {
			log.Println("Channel: sending", "<"+message+">", "to", address, ".")
		}
	*/
	// returns message ID but we currently don't use it
	_, err = channel.tox.FriendSendMessage(id, gotox.TOX_MESSAGE_TYPE_NORMAL, message)
	return err
}

/*
SendFile starts a file transfer to the given address. Will directly begin the
transfer!
*/
func (channel *Channel) SendFile(address string, path string, identification string, f OnDone) error {
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
	/*
		if channel.log {
			log.Println("Channel: sending file", identification, "to", address, ".")
		}
	*/
	// prepare send (file will be transmitted via filechunk)
	fileNumber, err := channel.tox.FileSend(id, gotox.TOX_FILE_KIND_DATA, size, nil, identification)
	if err != nil {
		file.Close()
		return err
	}
	// create transfer object
	channel.transfers[fileNumber] = transfer{
		file: file,
		size: size,
		done: f}
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
RemoveConnection removes a friend from the friendlist, effectivly terminating
the connection.
*/
func (channel *Channel) RemoveConnection(address string) error {
	publicKey, err := hex.DecodeString(address)
	if err != nil {
		return err
	}
	num, err := channel.tox.FriendByPublicKey(publicKey)
	if err != nil {
		return err
	}
	return channel.tox.FriendDelete(num)
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
			log.Println(tag, "Stopping background process!")
			channel.wg.Done()
			log.Println("Stopped!")
			return
		case <-time.Tick(intervall):
			err := channel.tox.Iterate()
			if err != nil {
				/* TODO what do we do here? Can we cleanly close the channel and
				catch the error further up? */
				log.Println(tag, "Run:", err)
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

// ---------------------------------CALLBACKS ----------------------------------

/*
onFriendRequest calls the appropriate callback, wrapping it sanely for our purposes.
*/
func (channel *Channel) onFriendRequest(_ *gotox.Tox, publicKey []byte, message string) {
	if channel.callbacks != nil {
		// strip key of NOSPAM - this is the only instance where it is passed here
		if len(publicKey) > 32 {
			publicKey = publicKey[:32]
		}
		channel.callbacks.OnNewConnection(hex.EncodeToString(publicKey), message)
	} else {
		log.Println(tag, "Error: callbacks are nil!")
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
			channel.callbacks.OnMessage(address, message)
		} else {
			log.Println(tag, "callbacks are nil!")
		}
	} else {
		log.Println(tag, "Invalid message type, ignoring!")
	}
}

/*
TODO comment
*/
func (channel *Channel) onFriendConnectionStatusChanges(_ *gotox.Tox, friendnumber uint32, connectionstatus gotox.ToxConnection) {
	if channel.log {
		log.Println(tag, "detected status change")
	}
	// if going offline do nothing
	if connectionstatus == gotox.TOX_CONNECTION_NONE {
		return
	}
	address, err := channel.addressOf(friendnumber)
	if err != nil {
		log.Println(tag, err)
		return
	}
	if channel.callbacks != nil {
		channel.callbacks.OnConnected(address)
	} else {
		log.Println(tag, "No callback registered!")
	}
}

/*
TODO comment
*/
func (channel *Channel) onFileRecvControl(_ *gotox.Tox, friendnumber uint32, filenumber uint32, fileControl gotox.ToxFileControl) {
	// we only explicitely need to handle cancel because we then have to remove resources
	if fileControl == gotox.TOX_FILE_CONTROL_CANCEL {
		log.Println(tag, "Transfer was canceled!")
		// free resources
		channel.transfers[filenumber].file.Close()
		delete(channel.transfers, filenumber)
	}
}

/*
TODO implement and comment
*/
func (channel *Channel) onFileRecv(_ *gotox.Tox, friendnumber uint32, filenumber uint32, kind gotox.ToxFileKind, filesize uint64, filename string) {
	// we're not interested in avatars
	if kind != gotox.TOX_FILE_KIND_DATA {
		log.Println(tag, "Ignoring non data file transfer!")
		return
	}
	// address
	address, err := channel.addressOf(friendnumber)
	if err != nil {
		log.Println(tag, err.Error())
		address = illegalAddress
	}
	// use callback to check whether to accept from Tinzenite
	accept, path := channel.callbacks.OnAllowFile(address, filename)
	if !accept {
		return
	}
	// accept file send request if we come to here
	channel.tox.FileControl(friendnumber, filenumber, gotox.TOX_FILE_CONTROL_RESUME)
	// create file at correct location
	/*TODO how are pause & resume handled?*/
	f, _ := os.Create(path)
	// Append f to the map[uint8]*os.File
	tran := channel.transfers[filenumber]
	tran.file = f
	tran.size = filesize
	channel.transfers[filenumber] = tran
}

/*
onFileRecvChunk is called when a chunk of a file is received. Writes the data to
the correct file.
*/
func (channel *Channel) onFileRecvChunk(_ *gotox.Tox, friendnumber uint32, filenumber uint32, position uint64, data []byte) {
	// Write data to the hopefully valid *File handle
	tran, exists := channel.transfers[filenumber]
	if exists {
		tran.file.WriteAt(data, (int64)(position))
	} else {
		log.Println("Transfer doesn't seem to exist!")
		return
	}
	// this means the file has been completey received
	if position == tran.size {
		// ensure file is written
		err := tran.file.Sync()
		if err != nil {
			log.Println("Disk error: " + err.Error())
			return
		}
		pathelements := strings.Split(tran.file.Name(), "/")
		tran.file.Close()
		// free resources
		delete(channel.transfers, filenumber)
		// callback with file name / identification
		address, _ := channel.addressOf(friendnumber)
		name := pathelements[len(pathelements)-1]
		path := strings.Join(pathelements, "/")
		/*TODO we have 2 callbacks of a kind here.. can this be improved?*/
		// execute done function
		tran.execute(true)
		// call callback
		channel.callbacks.OnFileReceived(address, path, name)
	}
}

/*
onFileChunkRequest is called when a chunk must be sent.
*/
func (channel *Channel) onFileChunkRequest(_ *gotox.Tox, friendNumber uint32, fileNumber uint32, position uint64, length uint64) {
	trans, ok := channel.transfers[fileNumber]
	// sanity check
	if !ok {
		log.Println(tag, "Failed to read from channel.transfers!")
		return
	}
	// recalculate length if near end of file
	if length+position > trans.size {
		length = trans.size - position
	}
	// if we're done
	if length == 0 {
		trans.file.Sync()
		trans.file.Close()
		trans.execute(true)
		delete(channel.transfers, fileNumber)
		// close everything and return
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
}
