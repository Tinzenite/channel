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
	transfers map[uint32]*transfer
	log       bool
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

	// prepare for file transfers
	channel.transfers = make(map[uint32]*transfer)

	// this decides whether we are initiating a new connection or using an existing one
	if toxdata == nil {
		log.Println("Channel:", "WARNING create called with empty ToxData.")
		// updated from gotox: nil options okay on first init
		options = nil
		init = true
	} else {
		options = &gotox.Options{
			IPv6Enabled:  true,
			UDPEnabled:   true,
			ProxyType:    gotox.TOX_PROXY_TYPE_NONE,
			ProxyHost:    "127.0.0.1",
			ProxyPort:    5555,
			StartPort:    0,
			EndPort:      0,
			TcpPort:      0,
			SaveDataType: gotox.TOX_SAVEDATA_TYPE_TOX_SAVE,
			SaveData:     toxdata}
		init = false
	}
	channel.tox, err = gotox.New(options)
	if err != nil {
		return nil, err
	}
	// if init, AFTER creating the tox instance, set these
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
	// register callbacks
	channel.callbacks = callbacks
	// now to run it:
	channel.wg.Add(1)
	channel.stop = make(chan bool, 0)
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
	// send stop signal
	channel.stop <- true
	// wait for it to close
	channel.wg.Wait()
	// kill tox
	channel.tox.Kill()
	// clean all file transfers
	for _, transfer := range channel.transfers {
		transfer.Close(false)
	}
	if channel.log {
		log.Println(tag, "Closed.")
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
		online, err := channel.IsAddressOnline(address)
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
	if ok, err := channel.IsAddressOnline(address); !ok {
		if err != nil {
			return err
		}
		return errOffline
	}
	// find friend id to send to
	id, err := channel.friendNumberOf(address)
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
	if ok, _ := channel.IsAddressOnline(address); !ok {
		return errOffline
	}
	// find friend id to send to
	id, err := channel.friendNumberOf(address)
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
	stat, err := file.Stat()
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
	// create transfer object
	channel.transfers[fileNumber] = createTransfer(path, id, file, size, f)
	return nil
}

/*
CancelFileTransfer cancels the file transfer that is writting to the given path.
*/
func (channel *Channel) CancelFileTransfer(path string) error {
	// find fileNumber & transfer via file name
	var found bool
	var fileNumber uint32
	var transfer *transfer
	for thisNumber, thisTransfer := range channel.transfers {
		if thisTransfer.path == path {
			found = true
			fileNumber = thisNumber
			transfer = thisTransfer
			break
		}
	}
	// if none found return error
	if !found {
		return errTransferNotFound
	}
	// cancel transfer
	channel.tox.FileControl(transfer.friend, fileNumber, gotox.TOX_FILE_CONTROL_CANCEL)
	// close transfer
	transfer.Close(false)
	// remove object
	delete(channel.transfers, fileNumber)
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
	num, err := channel.friendNumberOf(address)
	if err != nil {
		return err
	}
	return channel.tox.FriendDelete(num)
}

/*
IsAddressOnline checks whether the given address is currently reachable.
*/
func (channel *Channel) IsAddressOnline(address string) (bool, error) {
	num, err := channel.friendNumberOf(address)
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
	num, err := channel.friendNumberOf(address)
	if err != nil {
		return "", err
	}
	name, err := channel.tox.FriendGetName(num)
	if err != nil {
		return "", err
	}
	return name, nil
}

/*
IsOnline referes to the connection status of the channel.
*/
func (channel *Channel) IsOnline() (bool, error) {
	status, err := channel.tox.SelfGetConnectionStatus()
	if err != nil {
		return false, err
	}
	if status != gotox.TOX_CONNECTION_NONE {
		return true, nil
	}
	return false, nil
}

/*
ActiveTransfers returns a map of file names and associated percentage done. By
polling it regularly this can be used to offer feedback on long transfers.
*/
func (channel *Channel) ActiveTransfers() map[string]int {
	list := make(map[string]int)
	for _, transfer := range channel.transfers {
		list[transfer.file.Name()] = transfer.Percentage()
	}
	return list
}

// --- private methods here ---

/*
run is the background go routine method that keeps the Tox instance iterating
until Close() is called.
*/
func (channel *Channel) run() {
	// log when stopping background process (even if returning error)
	defer func() { log.Println(tag, "Background process stopped.") }()
	// read ToxNodes
	toxNodes, err := toxdynboot.FetchAlive(1 * time.Second)
	if err != nil {
		log.Println(tag, "Fetching ToxNodes for Tox failed!", err)
	}
	// warn if less than 5 ToxNodes (even 0)
	if len(toxNodes) < 5 {
		log.Println(tag, "WARNING: Too few ToxNodes!", len(toxNodes), " ToxNodes found.")
	}
	// TODO: how to use tox.GetIterationIntervall to update ticker without performance loss? For now: just tick every 50ms
	iterateTicker := time.Tick(50 * time.Millisecond)
	// we check if we have to bootstrap every 10 seconds (this will allow clean reconnect if we ever loose internet)
	bootTicker := time.Tick(10 * time.Second) // FIXME: if first start we can bootstrap every 5 seconds until connected
	// endless loop until close is called for tox.Iterate
	for {
		// select whether we have to close, iterate, or check online status
		select {
		case <-channel.stop:
			// close wg and return (we're done)
			channel.wg.Done()
			return
		case <-iterateTicker:
			// try to iterate
			err := channel.tox.Iterate()
			if err != nil {
				log.Println(tag, "Run:", err)
			}
		case <-bootTicker:
			// don't bootstrap if channel is online
			online, _ := channel.IsOnline()
			if online {
				break
			}
			log.Println(tag, "Bootstrapping to Tox network with", len(toxNodes), "nodes.")
			// try to bootstrap to all nodes. Better: random set of 4 nodes, but meh.
			for _, node := range toxNodes {
				err := channel.tox.Bootstrap(node.IPv4, node.Port, node.PublicKey)
				if err != nil {
					log.Println(tag, "Bootstrap error for a node:", err)
				}
			} // bootstrap for
		} // select
	} // endless for
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
friendNumberOf the given address.
*/
func (channel *Channel) friendNumberOf(address string) (uint32, error) {
	publicKey, err := hex.DecodeString(address)
	if err != nil {
		return 0, err
	}
	return channel.tox.FriendByPublicKey(publicKey)
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
		channel.callbacks.OnFriendRequest(hex.EncodeToString(publicKey), message)
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
			channel.callbacks.OnMessage(address, message)
		} else {
			log.Println(tag, "No callback for OnMessage registered!")
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
	if channel.callbacks != nil {
		address, err := channel.addressOf(friendnumber)
		if err != nil {
			log.Println(tag, "OnConnected:", err)
			return
		}
		channel.callbacks.OnConnected(address)
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
			channel.callbacks.OnFileCanceled(address, trans.path)
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
	// use callback to check whether to accept from Tinzenite
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
		// TODO FIXME we run into this a lot... especially with large files
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
			channel.callbacks.OnFileReceived(address, path, name)
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
