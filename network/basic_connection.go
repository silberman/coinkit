package network

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"coinkit/util"
)

// How frequently in seconds to send keepalive pings
const keepalive = 10

// A BasicConnection represents a two-way message channel.
// You can close it at any point, and it will close itself if it detects
// network problems.
type BasicConnection struct {
	conn     net.Conn
	handler  func(*util.SignedMessage)
	outbox   chan *util.SignedMessage
	inbox    chan *util.SignedMessage
	quit     chan bool
	closed   bool
	quitOnce sync.Once
}

// NewBasicConnection creates a new logical connection given a network connection.
// inbox is the channel to send messages to.
func NewBasicConnection(conn net.Conn, inbox chan *util.SignedMessage) *BasicConnection {
	c := &BasicConnection{
		conn:   conn,
		outbox: make(chan *util.SignedMessage, 100),
		inbox:  inbox,
		quit:   make(chan bool),
		closed: false,
	}
	go c.runIncoming()
	go c.runOutgoing()
	return c
}

func (c *BasicConnection) Close() {
	c.quitOnce.Do(func() {
		c.closed = true
		close(c.quit)
	})
}

func (c *BasicConnection) IsClosed() bool {
	return c.closed
}

func (c *BasicConnection) runIncoming() {
	for {
		// Wait for 2x the keepalive period
		c.conn.SetReadDeadline(time.Now().Add(2 * keepalive * time.Second))
		response, err := util.ReadSignedMessage(c.conn)
		if c.closed {
			break
		}
		if err != nil {
			log.Printf("connection error: %+c", err)
			c.Close()
			break
		}
		if response != nil {
			c.inbox <- response
		}
	}
}

func (c *BasicConnection) runOutgoing() {
	for {
		var message *util.SignedMessage
		timer := time.NewTimer(time.Duration(keepalive * time.Second))
		select {
		case <-c.quit:
			return
		case <-timer.C:
			// Send a keepalive ping
			message = nil
		case message = <-c.outbox:
		}

		fmt.Fprintf(c.conn, util.SignedMessageToLine(message))
	}
}

// Send sends a message, but only if the queue is not full.
// It returns whether the message entered the outbox.
func (c *BasicConnection) Send(message *util.SignedMessage) bool {
	select {
	case c.outbox <- message:
		return true
	default:
		log.Printf("Connection outbox overloaded, dropping message")
		return false
	}
}

// Receive returns the next message that is received.
// It returns nil if the connection gets closed before a message is read.
func (c *BasicConnection) Receive() *util.SignedMessage {
	select {
	case m := <-c.inbox:
		return m
	case <-c.quit:
		return nil
	}
}

// QuitChannel returns a channel that gets closed once, when the channel shuts down.
func (c *BasicConnection) QuitChannel() chan bool {
	return c.quit
}
