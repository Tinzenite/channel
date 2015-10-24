package channel

import (
	"errors"
	"log"
	"sync"

	"github.com/codedust/go-tox"
)

/*
Channel is a wrapper of the gotox wrapper that creates and manages the underlying Tox
instance.
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
