package channel

import (
	"encoding/hex"
	"log"
	"os"

	"github.com/codedust/go-tox"
)

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
		transfer.Close(StCanceled)
	}
	log.Println(tag, "Closed.")
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

TODO: FIXME / NOTE: reimplement sending so that only ever one file is sent to an
address at the same time. Also implement timeouts so that the queue can't block.
*/
func (channel *Channel) SendFile(address string, path string, identification string, f func(status State)) error {
	if ok, _ := channel.IsAddressOnline(address); !ok {
		return errOffline
	}
	// find friend id to send to
	friendID, err := channel.friendNumberOf(address)
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
	// create transfer object
	transfer := createTransfer(path, identification, friendID, file, size, f)
	// write to queue
	channel.sending.add(address, transfer)
	// trigger sending to that address
	channel.triggerSend(address)
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
	transfer.Close(StCanceled)
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
