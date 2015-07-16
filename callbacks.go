package channel

// Callbacks for external wrapped access.
type Callbacks interface {
	/*CallbackNewConnection is called on a Tox friend request.*/
	OnNewConnection(address, message string)
	/*CallbackMessage is called on an incomming message.*/
	OnMessage(address, message string)
	/*CallbackAllowFile is called when a file transfer is wished. Returns the
	permission as bool and the path where to write the file.*/
	OnAllowFile(address, identification string) (bool, string)
	/*CallbackFileReceived is called once the file has been successfully
	received completely.*/
	OnFileReceived(address, identification string)
}
