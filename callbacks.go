package channel

// Callbacks for external wrapped access.
type Callbacks interface {
	/*OnNewConnection is called on a Tox friend request.*/
	OnNewConnection(address, message string)
	/*OnMessage is called on an incomming message.*/
	OnMessage(address, message string)
	/*OnAllowFile is called when a file transfer is wished. Returns the
	permission as bool and the path where to write the file.*/
	OnAllowFile(address, name string) (bool, string)
	/*OnFileReceived is called once the file has been successfully
	received completely.*/
	OnFileReceived(address, path, name string)
	/*OnFileCanceled is called if a file transfer is canceled by the other side.*/
	OnFileCanceled(address, path string)
	/*OnConnected is called when a friend comes online.*/
	OnConnected(address string)
}
