package channel

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/codedust/go-tox"
	"github.com/xamino/tox-dynboot"
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
