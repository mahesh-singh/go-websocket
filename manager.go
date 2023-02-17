package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	websocketUpgrader = websocket.Upgrader{
		CheckOrigin:     checkOrigin,
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

var (
	ErrEventNotSupported = errors.New("this event is not supported")
)

type Manager struct {
	clients ClientList
	sync.RWMutex
	otps     RetentionMap
	handlers map[string]EventHandler
}

func NewManager(ctx context.Context) *Manager {
	log.Println("new manager starting")
	m := &Manager{
		clients:  make(ClientList),
		handlers: make(map[string]EventHandler),
		otps:     NewRetentionMap(ctx, 5*time.Second),
	}
	log.Println("new manager before setting up event handler")
	m.setupEventHandler()
	log.Println("new manager before returning manager")
	return m
}

func (m *Manager) setupEventHandler() {
	m.handlers[EventSendMessage] = SendMessage
}

func SendMessage(event Event, c *Client) error {
	var chatEvent SendMessageEvent
	log.Println(string(event.Payload))
	if err := json.Unmarshal(event.Payload, &chatEvent); err != nil {
		return fmt.Errorf("bad payload in request: %v", err)
	}
	var broadMessage NewMessageEvent
	broadMessage.Sent = time.Now()
	broadMessage.Message = chatEvent.Message
	broadMessage.From = chatEvent.From

	data, err := json.Marshal(broadMessage)
	if err != nil {
		return fmt.Errorf("Failed to marshal the event: &v", err)
	}

	outgoingEvent := Event{
		Payload: data,
		Type:    EventNewMessage,
	}

	for client := range c.manager.clients {
		client.egress <- outgoingEvent
	}

	return nil
}

func (m *Manager) routeEvent(event Event, c *Client) error {
	if handler, ok := m.handlers[event.Type]; ok {
		if err := handler(event, c); err != nil {
			return err
		}
		return nil
	} else {
		return ErrEventNotSupported
	}
}

func (m *Manager) serveWS(w http.ResponseWriter, r *http.Request) {
	//upgrade the connection

	otp := r.URL.Query().Get("otp")

	if otp == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if !m.otps.VerifyOTP(otp) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	conn, err := websocketUpgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Println(err)
		return
	}
	log.Printf("connection upgraded")
	client := NewClient(conn, m)
	m.addClient(client)

	//Start client process
	go client.readMessage()
	go client.writeMessage()
}

func (m *Manager) addClient(client *Client) {
	m.Lock()
	defer m.Unlock()

	m.clients[client] = true

}

func (m *Manager) loginHandler(w http.ResponseWriter, r *http.Request) {
	type userLoginRequest struct {
		UserName string `json:"username"`
		Password string `json:"password"`
	}

	var req userLoginRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.UserName == "mahesh" && req.Password == "123" {
		type response struct {
			OTP string `json:otp`
		}

		otp := m.otps.NewOTP()

		resp := response{
			OTP: otp.Key,
		}

		data, err := json.Marshal(resp)

		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write(data)
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
}

func (m *Manager) removeClient(client *Client) {
	m.Lock()
	defer m.Unlock()

	if _, ok := m.clients[client]; ok {
		client.connection.Close()
		delete(m.clients, client)
	}
}

func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	switch origin {
	case "http://localhost:8080":
		return true
	default:
		return false
	}

}
