package jsonnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/eapache/channels"
	"github.com/gamejolt/joltron/broadcast"
	"github.com/gamejolt/joltron/network/messages/incoming"
	"github.com/gamejolt/joltron/network/messages/outgoing"
)

// Listener handles a network listener for connections that write and receive json.
type Listener struct {
	port     uint16
	listener net.Listener
	closed   bool

	mu                  sync.Mutex
	nextConnectionID    int
	connections         map[int]*Connection
	incomingMessageSend *channels.InfiniteChannel
	IncomingMessages    <-chan *IncomingMessage
	broadcaster         *broadcast.Broadcaster
}

// NewListener creates a new Listener
func NewListener(port uint16, listener net.Listener) (*Listener, error) {
	if port < 1024 {
		return nil, errors.New("Cannot listen on a port smaller than 1024")
	}

	if listener == nil {
		endpoint := fmt.Sprintf("0.0.0.0:%d", port)
		addr, err := net.ResolveTCPAddr("tcp", endpoint)
		if err != nil {
			return nil, err
		}

		listener, err = net.ListenTCP("tcp", addr)
		if err != nil {
			return nil, err
		}
	}

	handler := &Listener{
		port:                port,
		listener:            listener,
		connections:         make(map[int]*Connection),
		incomingMessageSend: channels.NewInfiniteChannel(),
		broadcaster:         broadcast.NewBroadcaster(),
	}
	handler.IncomingMessages = handler.newIncomingMessageChannel()

	go func() {
		for {
			conn, err := handler.listener.Accept()
			if err != nil {
				if handler.closed {
					break
				}
				log.Println(err.Error())
				continue
			}

			handler.addConnection(conn)
		}
	}()

	return handler, nil
}

// Port gets the listener's port
func (l *Listener) Port() uint16 {
	return l.port
}

// Connections return the current connections of the listeners.
// It returns a copy of the connections slice
func (l *Listener) Connections() []*Connection {
	l.mu.Lock()
	defer l.mu.Unlock()

	conns := []*Connection{}
	for _, conn := range l.connections {
		conns = append(conns, conn)
	}
	return conns
}

// ConnectionCount returns the number of connections this jsonnet currently holds
func (l *Listener) ConnectionCount() int {
	return len(l.connections)
}

func (l *Listener) addConnection(conn net.Conn) *Connection {
	l.mu.Lock()
	defer l.mu.Unlock()

	log.Printf("Client connection from %s\n", conn.RemoteAddr().String())
	jsonConn := NewConnection(l, conn)
	l.connections[jsonConn.id] = jsonConn
	l.nextConnectionID++

	l.broadcaster.Broadcast(jsonConn)
	return jsonConn
}

func (l *Listener) removeConnection(jsonConn *Connection) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	log.Printf("Client %s disconnected\n", jsonConn.connection.RemoteAddr().String())
	if _, ok := l.connections[jsonConn.id]; !ok {
		return nil
	}
	delete(l.connections, jsonConn.id)
	return jsonConn.Close()
}

func (l *Listener) newIncomingMessageChannel() <-chan *IncomingMessage {
	ch := make(chan *IncomingMessage)

	go func() {
		for {
			_msg, open := <-l.incomingMessageSend.Out()
			if !open {
				break
			}
			msg, ok := _msg.(*IncomingMessage)
			if !ok {
				break
			}

			l.mu.Lock()
			_, ok = l.connections[msg.connection.id]
			l.mu.Unlock()

			if ok {
				ch <- msg
			}
		}

		close(ch)
	}()

	return ch
}

// OnConnection returns a *broadcast.Subscriber that will receive instances of connections when they connect to the network.
// To wait for them to close use *Connection.Done()
func (l *Listener) OnConnection() (*broadcast.Subscriber, error) {
	return l.broadcaster.Join()
}

// Close closes the listener and all connections
func (l *Listener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.incomingMessageSend.Close()
	for _, connection := range l.connections {
		connection.Close()
	}
	l.closed = true
	return l.listener.Close()
}

// Connection handles a single connection to read and receive json on.
type Connection struct {
	id         int
	listener   *Listener
	connection net.Conn

	enc  *json.Encoder
	dec  *json.Decoder
	done chan interface{}
}

// NewConnection creates a new Connection
func NewConnection(listener *Listener, conn net.Conn) *Connection {
	jc := &Connection{
		id:         listener.nextConnectionID,
		listener:   listener,
		connection: conn,
		dec:        json.NewDecoder(conn),
		enc:        json.NewEncoder(conn),
		done:       make(chan interface{}),
	}

	go func() {
		defer jc.listener.removeConnection(jc)

		for {
			payload, msgID, err := jc.decodeIncomingMessage()
			if err != nil {
				jc.enc.Encode(outgoing.OutMsgResult{
					Success: false,
					Error:   "Invalid input",
				})
				break
			}

			jc.listener.incomingMessageSend.In() <- &IncomingMessage{
				connection: jc,
				MsgID:      msgID,
				Payload:    payload,
			}
		}
	}()

	return jc
}

func (jc *Connection) decodeIncomingMessage() (interface{}, string, error) {
	return incoming.DecodeMsg(jc.dec)
}

func (jc *Connection) encodeOutgoingMessage(msg interface{}, msgID string) error {
	return outgoing.EncodeMsg(jc.enc, msg, msgID)
}

// Close closes the json connection
func (jc *Connection) Close() error {
	close(jc.done)
	return jc.connection.Close()
}

// Done returns a channel that is closed when the connection is terminated
func (jc *Connection) Done() <-chan interface{} {
	return jc.done
}

// IncomingMessage is a parsed json message received on a Connection.
// It is used to send back results over the same connection it was received.
type IncomingMessage struct {
	connection *Connection
	MsgID      string
	Payload    interface{}
}

// Respond sends back a message on the connection
func (m *IncomingMessage) Respond(msg interface{}) error {
	m.connection.listener.mu.Lock()
	defer m.connection.listener.mu.Unlock()

	conn, ok := m.connection.listener.connections[m.connection.id]
	if !ok {
		return errors.New("Connection is already closed")
	}

	if err := conn.encodeOutgoingMessage(msg, m.MsgID); err != nil {
		log.Fatalf("Failed to write msg to client %s. Error: %s", conn.connection.RemoteAddr().String(), err.Error())
		return err
	}

	return nil
}

// Broadcast sends a message to all connected clients
func (l *Listener) Broadcast(msg interface{}) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, connection := range l.connections {
		if err := connection.encodeOutgoingMessage(msg, ""); err != nil {
			log.Fatalf("Failed to write msg to client %s. Error: %s", connection.connection.RemoteAddr().String(), err.Error())
			continue
		}
	}

	return nil
}
