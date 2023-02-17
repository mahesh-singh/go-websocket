package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

var (
	pongWait     = 10 * time.Second
	pingInterval = (pongWait * 9) / 10
)

type ClientList map[*Client]bool

type Client struct {
	connection *websocket.Conn
	manager    *Manager

	//egress is used to avoid concurrent write on the websocket connection
	egress chan Event
}

func NewClient(conn *websocket.Conn, manager *Manager) *Client {
	return &Client{connection: conn,
		manager: manager,
		egress:  make(chan Event),
	}
}

func (c *Client) readMessage() {
	defer func() {
		c.manager.removeClient(c)
	}()
	if err := c.connection.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Println("err")
		return
	}
	c.connection.SetReadLimit(512)
	c.connection.SetPongHandler(c.pongHandler)
	for {
		_, payload, err := c.connection.ReadMessage()

		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error reading message: &v", err)
			}
			break
		}
		var request Event
		if err := json.Unmarshal(payload, &request); err != nil {
			log.Printf("Error un marshaling message: %v", err)
			break
		}

		if err := c.manager.routeEvent(request, c); err != nil {
			log.Printf("error handling message: %v", err)
		}
	}
}

func (c *Client) writeMessage() {
	defer func() {
		c.manager.removeClient(c)
	}()
	ticker := time.NewTicker(pingInterval)
	for {

		select {
		case message, ok := <-c.egress:
			if !ok {
				if err := c.connection.WriteMessage(websocket.CloseMessage, nil); err != nil {
					log.Println("Connection closed: ", err)
				}
				return
			}
			data, err := json.Marshal(message)
			if err != nil {
				log.Println(err)
				return // closes the connection, should we really
			}
			if err := c.connection.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("Failed to send message: %v", err)
			}

			log.Println("Message sent")
		case <-ticker.C:
			log.Println("ping")
			if err := c.connection.WriteMessage(websocket.PingMessage, []byte(``)); err != nil {
				log.Println("Error while writing ping message: ", err)
				return
			}
		}

	}
}

func (c *Client) pongHandler(pongMsg string) error {
	log.Println("pong")
	return c.connection.SetReadDeadline(time.Now().Add(pongWait))
}
