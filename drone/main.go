package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/pion/mediadevices/pkg/driver/camera"
	_ "github.com/pion/mediadevices/pkg/driver/microphone"
	"github.com/pion/webrtc/v3"
)

var iceConfig webrtc.Configuration
var rtpListener *net.UDPConn
var pc *webrtc.PeerConnection

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
	closeCh  chan struct{}
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
		return err
	}
	err = ws.Conn.WriteMessage(websocket.TextMessage, msgJson)
	if err != nil {
		return err
	}
	return nil
}

func (ws *Websocket) Register(channel string, callback registryFunc) {
	ws.registry[channel] = callback
}

func (ws *Websocket) Propagate(channel string, data string) error {
	callback, ok := ws.registry[channel]
	if !ok {
		return nil
	}
	err := callback(data)
	if err != nil {
		return err
	}
	return nil
}

func (ws *Websocket) Connect(host string) error {
	u := url.URL{Scheme: "ws", Host: host, Path: "/"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	ws.Conn = c

	go func() {
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
					log.Fatalln("unmarshal:", err)
				}
				err = ws.Propagate(msg.Channel, msg.Data)
				if err != nil {
					log.Fatalln("propagate:", err)
				}
			}
		}
	}()
	return nil
}

func (ws *Websocket) Close() error {
	close(ws.closeCh)
	err := ws.Conn.Close()
	if err != nil {
		return err
	}
	return nil
}

type RTCPeerConnection struct {
	Conn    *webrtc.PeerConnection
	closeCh chan struct{}
}

func NewRTCPeerConnection(ws *Websocket) *RTCPeerConnection {
	pc := &RTCPeerConnection{
		closeCh: make(chan struct{}),
	}
	var err error
	pc.Conn, err = webrtc.NewPeerConnection(iceConfig)
	if err != nil {
		log.Fatalln("newrtc", err)
	}

	pc.Conn.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Println("Connection State has changed:", state.String())

		// TODO: do not close pc on all these states
		// trigger ice restart
		//  https://developer.mozilla.org/en-US/docs/Web/API/RTCPeerConnection/iceConnectionState
		if state == webrtc.ICEConnectionStateFailed {
			if err := pc.Close(); err != nil {
				log.Fatalln("close:", err)
			}
			pc = NewRTCPeerConnection(ws)
		}
	})

	pc.Conn.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}

		iceJson, err := json.Marshal(c.ToJSON())
		if err != nil {
			log.Fatalln("marshal:", err)
		}

		err = ws.Emit("iceCandidate", string(iceJson))
		if err != nil {
			log.Fatalln("emit:", err)
		}
	})

	// Create a video track
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	if err != nil {
		log.Fatalln("newtrack:", err)
	}
	_, err = pc.Conn.AddTrack(track)
	if err != nil {
		log.Fatalln("addtrack:", err)
	}

	go func() {
		// Read RTP packets forever and send them to the WebRTC Client
		inboundRTPPacket := make([]byte, 1600) // UDP MTU
		for {
			select {
			case <-pc.closeCh:
				return
			default:
				n, _, err := rtpListener.ReadFrom(inboundRTPPacket)
				if err != nil {
					log.Fatalln("readfrom:", err)
				}

				if _, err = track.Write(inboundRTPPacket[:n]); err != nil {
					if errors.Is(err, io.ErrClosedPipe) {
						log.Println("Peer connection closed")
						return
					}
					log.Fatalln("write:", err)
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
		log.Fatalln("marshal:", err)
	}
	err = ws.Emit("match", string(dataJson))
	if err != nil {
		log.Fatalln("emit:", err)
	}
	return pc
}

func (pc *RTCPeerConnection) Close() error {
	// close all associated goroutines
	log.Println("closing pc channel")
	close(pc.closeCh)
	err := pc.Conn.Close()
	if err != nil {
		return err
	}
	return nil
}

func main() {
	host := os.Getenv("HOST")
	if host == "" {
		log.Fatalln("Forgot to set HOST environment variable")
	}

	ws := NewWebsocket()
	err := ws.Connect(host)
	if err != nil {
		log.Fatalln("connect:", err)
	}
	defer ws.Close()

	pc := NewRTCPeerConnection(ws)

	ws.Register("connected", func(data string) error {
		err := ws.Emit("connected", "Hello from drone")
		if err != nil {
			return err
		}
		return nil
	})

	ws.Register("begin", func(data string) error {
		offer, err := pc.Conn.CreateOffer(nil)
		if err != nil {
			return err
		}
		offerJson, err := json.Marshal(offer)
		if err != nil {
			return err
		}
		err = ws.Emit("description", string(offerJson))
		if err != nil {
			return err
		}
		err = pc.Conn.SetLocalDescription(offer)
		if err != nil {
			return err
		}
		return nil
	})

	ws.Register("clientsOnline", func(data string) error {
		return nil
	})

	ws.Register("message", func(data string) error {
		log.Println("Message recv:", data)
		return nil
	})

	ws.Register("iceCandidate", func(data string) error {
		var ice webrtc.ICECandidateInit
		err := json.Unmarshal([]byte(data), &ice)
		if err != nil {
			return err
		}
		err = pc.Conn.AddICECandidate(ice)
		if err != nil {
			return err
		}
		return nil
	})

	ws.Register("description", func(data string) error {
		var desc webrtc.SessionDescription
		err := json.Unmarshal([]byte(data), &desc)
		if err != nil {
			return err
		}
		err = pc.Conn.SetRemoteDescription(desc)
		if err != nil {
			return err
		}
		return nil
	})

	ws.Register("disconnect", func(data string) error {
		log.Println("Received disconnect request")
		pc.Close()
		pc = NewRTCPeerConnection(ws)
		return nil
	})

	select {}
}
