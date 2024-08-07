package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/pion/mediadevices/pkg/driver/camera"
	"github.com/pion/webrtc/v3"
)

var iceConfig webrtc.Configuration
var rtpListener *net.UDPConn
var pc *RTCPeerConnection

func init() {
	iceConfig = webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
	// Open a UDP Listener for RTP Packets on port 5004
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5004})
	if err != nil {
		panic(err)
	}
	// Increase the UDP receive buffer size
	// Default UDP buffer sizes vary on different operating systems
	bufferSize := 300000 // 300KB
	err = listener.SetReadBuffer(bufferSize)
	if err != nil {
		panic(err)
	}
	rtpListener = listener
}

type registryFunc func(string) error

type WebsocketMsg struct {
	Channel string `json:"channel"`
	Data    string `json:"data"`
}

type Websocket struct {
	Conn     *websocket.Conn
	registry map[string]registryFunc
	lock     sync.Mutex
	cnLock   sync.Mutex
	closeCh  chan struct{}
	wg       sync.WaitGroup
}

func NewWebsocket() *Websocket {
	return &Websocket{
		registry: make(map[string]registryFunc),
		closeCh:  make(chan struct{}),
	}
}

func (ws *Websocket) Emit(channel string, data string) error {
	msgJson, err := json.Marshal(WebsocketMsg{
		Channel: channel,
		Data:    data,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	// prevent concurrent writes
	ws.cnLock.Lock()
	err = ws.Conn.WriteMessage(websocket.TextMessage, msgJson)
	ws.cnLock.Unlock()
	if err != nil {
		return fmt.Errorf("wswrite: %w", err)
	}
	return nil
}

func (ws *Websocket) Register(channel string, callback registryFunc) {
	ws.lock.Lock()
	ws.registry[channel] = callback
	ws.lock.Unlock()
}

func (ws *Websocket) Propagate(channel string, data string) error {
	ws.lock.Lock()
	callback, ok := ws.registry[channel]
	ws.lock.Unlock()
	if !ok {
		return nil
	}
	err := callback(data)
	if err != nil {
		return fmt.Errorf("callback: %w", err)
	}
	return nil
}

func (ws *Websocket) Connect(scheme string, host string) error {
	u := url.URL{Scheme: scheme, Host: host, Path: "/"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	ws.Conn = c

	ws.wg.Add(1)
	go func() {
		defer ws.wg.Done()
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ws.closeCh:
				return
			case <-ticker.C:
				err := ws.Emit("clientsOnline", "")
				if err != nil {
					log.Println("emit:", err)
					return
				}
			default:
				_, msgJson, err := ws.Conn.ReadMessage()
				if err != nil {
					log.Println("read:", err)
					return
				}
				var msg WebsocketMsg
				err = json.Unmarshal([]byte(msgJson), &msg)
				if err != nil {
					log.Println("unmarshal:", err)
				}
				err = ws.Propagate(msg.Channel, msg.Data)
				if err != nil {
					log.Println("propagate:", err)
				}
			}
		}
	}()
	return nil
}

func (ws *Websocket) Close() error {
	close(ws.closeCh)
	ws.wg.Wait()
	err := ws.Conn.Close()
	if err != nil {
		return fmt.Errorf("wsclose: %w", err)
	}
	return nil
}

type RTCPeerConnection struct {
	Conn    *webrtc.PeerConnection
	closeCh chan struct{}
	wg      sync.WaitGroup
}

func initNewRTCPeerConnection(ws *Websocket) {
	pc = &RTCPeerConnection{
		closeCh: make(chan struct{}),
	}
	var err error
	pc.Conn, err = webrtc.NewPeerConnection(iceConfig)
	if err != nil {
		log.Println("newrtc", err)
		err := pc.Close()
		if err != nil {
			log.Println("pclose:", err)
		}
		initNewRTCPeerConnection(ws)
	}

	log.Println("Created new rtc", pc)
	pc.Conn.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Println("Connection State has changed:", state.String())

		// TODO: do not close pc on all these states
		// trigger ice restart
		//  https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/iceConnectionState
		if state == webrtc.ICEConnectionStateDisconnected || state == webrtc.ICEConnectionStateFailed {
			offer, err := pc.Conn.CreateOffer(&webrtc.OfferOptions{ICERestart: true})
			if err != nil {
				log.Println("restart:", err, pc)
				resetPeerConnection(ws)
			}
			offerJson, err := json.Marshal(offer)
			if err != nil {
				log.Println("marshal:", err)
				resetPeerConnection(ws)
			}
			err = ws.Emit("description", string(offerJson))
			if err != nil {
				log.Println("emit:", err)
				resetPeerConnection(ws)
			}
			err = pc.Conn.SetLocalDescription(offer)
			if err != nil {
				log.Println("setlocaldesc:", err)
				resetPeerConnection(ws)
			}
			log.Println("Trying ice restart")
		}
	})

	pc.Conn.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}

		iceJson, err := json.Marshal(c.ToJSON())
		if err != nil {
			log.Println("marshal:", err)
		}

		err = ws.Emit("iceCandidate", string(iceJson))
		if err != nil {
			log.Println("emit:", err)
		}
	})

	// Create a video track
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	if err != nil {
		log.Println("newtrack:", err)
	}
	_, err = pc.Conn.AddTrack(track)
	if err != nil {
		log.Println("addtrack:", err)
	}

	pc.wg.Add(1)
	go func() {
		defer pc.wg.Done()
		// Read RTP packets forever and send them to the WebRTC Client
		inboundRTPPacket := make([]byte, 1600) // UDP MTU
		for {
			select {
			case <-pc.closeCh:
				return
			default:
				n, _, err := rtpListener.ReadFrom(inboundRTPPacket)
				if err != nil {
					log.Println("readfrom:", err)
				}

				if _, err = track.Write(inboundRTPPacket[:n]); err != nil {
					if errors.Is(err, io.ErrClosedPipe) {
						log.Println("Peer connection closed")
						return
					}
					log.Println("write:", err)
				}
			}
		}
	}()

	dataJson, err := json.Marshal(struct {
		Type string `json:"type"`
		Id   string `json:"id"`
	}{
		Type: "drone",
		Id:   "droneId",
	})
	if err != nil {
		log.Println("marshal:", err)
	}
	err = ws.Emit("match", string(dataJson))
	if err != nil {
		log.Println("emit:", err)
	}
}

func (pc *RTCPeerConnection) Close() error {
	// close all associated goroutines
	log.Println("closing pc channel")
	close(pc.closeCh)
	pc.wg.Wait()
	err := pc.Conn.Close()
	if err != nil {
		return fmt.Errorf("pcclose: %w", err)
	}
	return nil
}

func resetPeerConnection(ws *Websocket) {
	log.Println("Resetting rtc")
	err := pc.Close()
	if err != nil {
		log.Println("pcclose:", err)
	}
	initNewRTCPeerConnection(ws)
}

func main() {
	host := os.Getenv("HOST")
	scheme := os.Getenv("SCHEME")
	if host == "" {
		log.Fatalln("Forgot to set HOST environment variable")
	}
	if scheme == "" {
		log.Fatalln("Forgot to set SCHEME environment variable")
	}

	ws := NewWebsocket()
	err := ws.Connect(scheme, host)
	if err != nil {
		log.Fatalln("connect:", err)
	}
	defer ws.Close()

	initNewRTCPeerConnection(ws)

	ws.Register("connected", func(data string) error {
		err := ws.Emit("connected", "Hello from drone")
		if err != nil {
			return fmt.Errorf("emit: %w", err)
		}
		return nil
	})

	ws.Register("begin", func(data string) error {
		offer, err := pc.Conn.CreateOffer(nil)
		if err != nil {
			log.Println("createoffer:", err, pc)
			resetPeerConnection(ws)
		}
		offerJson, err := json.Marshal(offer)
		if err != nil {
			log.Println("marshal:", err)
			resetPeerConnection(ws)
		}
		err = ws.Emit("description", string(offerJson))
		if err != nil {
			log.Println("emit:", err)
			resetPeerConnection(ws)
		}
		err = pc.Conn.SetLocalDescription(offer)
		if err != nil {
			log.Println("setlocaldesc:", err)
			resetPeerConnection(ws)
		}
		return nil
	})

	ws.Register("clientsOnline", func(data string) error {
		return nil
	})

	ws.Register("msg", func(data string) error {
		log.Println("Message recv:", data)
		return nil
	})

	ws.Register("iceCandidate", func(data string) error {
		var ice webrtc.ICECandidateInit
		err := json.Unmarshal([]byte(data), &ice)
		if err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		err = pc.Conn.AddICECandidate(ice)
		if err != nil {
			return fmt.Errorf("addice: %w", err)
		}
		return nil
	})

	ws.Register("description", func(data string) error {
		var desc webrtc.SessionDescription
		err := json.Unmarshal([]byte(data), &desc)
		if err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		err = pc.Conn.SetRemoteDescription(desc)
		if err != nil {
			log.Println("setremovedesc:", err)
			resetPeerConnection(ws)
		}
		return nil
	})

	ws.Register("disconnect", func(data string) error {
		log.Println("Received disconnect request")
		resetPeerConnection(ws)
		return nil
	})

	select {}
}
