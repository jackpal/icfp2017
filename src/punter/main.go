package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
)

var server = flag.String("server", "punter.inf.ed.ac.uk", "server ip")
var port = flag.Int("port", 9001, "server port")
var name = flag.String("name", "iris punter", "bot name")

type HandshakeRequest struct {
	Me string `json:"me"`
}

type HandshakeResponse struct {
	You string `json:"you"`
}

type PunterID int
type SiteID int

type Site struct {
	ID SiteID `json:"id"`
}

type River struct {
	Source SiteID `json:"source"`
	Target SiteID `json:"target"`
}

type Map struct {
	Sites  []Site   `json:"sites"`
	Rivers []River  `json:"rivers"`
	Mines  []SiteID `json:"mines"`
}

type SetupRequest struct {
	Punter  PunterID `json:"punter"`
	Punters int      `json:"punters"`
	Map     Map      `json:"map"`
}

type SetupResponse struct {
	Ready PunterID `json:"ready"`
}

type Claim struct {
	Punter PunterID `json:"punter"`
	Source SiteID   `json:"source"`
	Target SiteID   `json:"target"`
}

type Pass struct {
	Punter PunterID `json:"punter"`
}

// Poor man's union type. Only one of Claim or Pass is non-null
type Move struct {
	Claim *Claim `json:"claim",omitempty`
	Pass  *Pass  `json:"pass",omitempty`
}

type Moves struct {
	Moves []Move `json:"moves"`
}

type ServerMove struct {
	Move Moves `json:"move"`
}

func findServer() (conn net.Conn, err error) {
	p := *port
	serverAddress := fmt.Sprintf("%s:%d", *server, p)
	log.Printf("Trying %s", serverAddress)
	conn, err = net.Dial("tcp", serverAddress)
	if err == nil {
		return
	}
	log.Fatal()
	return
}

func send(conn io.Writer, d interface{}) (err error) {
	var b []byte
	buf := bytes.NewBuffer(nil)
	err = json.NewEncoder(buf).Encode(d)
	if err != nil {
		return
	}
	b = buf.Bytes()
	msg := fmt.Sprintf("%d:%s", len(b), b)
	// log.Printf("Sending: %s", msg)
	_, err = conn.Write([]byte(msg))
	if err != nil {
		return
	}
	return err
}

func receive(conn io.Reader, d interface{}) (err error) {
	var i int
	_, err = fmt.Fscanf(conn, "%d:", &i)
	if err != nil {
		return
	}
	// log.Printf("Reading %d bytes", i)
	b1 := make([]byte, i)
	offset := 0
	for offset < i {
		var n int
		n, err = conn.Read(b1[offset:])
		if err != nil {
			return
		}
		offset += n
	}
	// log.Printf("Bytes: %d %v", len(b1), b1)
	// listen for reply
	err = json.Unmarshal(b1, d)
	return err
}

func handshake(conn io.ReadWriter) (err error) {
	handshakeRequest := HandshakeRequest{*name}
	err = send(conn, &handshakeRequest)
	if err != nil {
		return
	}

	// log.Printf("Waiting for reply")
	// listen for reply
	var handshakeResponse HandshakeResponse
	err = receive(conn, &handshakeResponse)
	if err != nil {
		return
	}
	// fmt.Printf("response %v\n", handshakeResponse)
	return
}

func setup(conn io.ReadWriter) (setupRequest SetupRequest, err error) {
	err = receive(conn, &setupRequest)
	if err != nil {
		return
	}
	setupResponse := SetupResponse{setupRequest.Punter}
	err = send(conn, &setupResponse)
	return
}

func main() {
	flag.Parse()
	err := onlineMode()
	if err != nil {
		log.Fatal(err)
	}
}

func onlineMode() (err error) {
	conn, err := findServer()
	if err != nil {
		return
	}
	log.Printf("connected")
	err = handshake(conn)
	if err != nil {
		return
	}
	log.Printf("handshake succeeded")

	setupRequest, err := setup(conn)
	if err != nil {
		return
	}
	log.Printf("Setup %v", setupRequest)

	for {
		var serverMove ServerMove
		err = receive(conn, &serverMove)
		if err != nil {
			return
		}
		log.Printf("Server move %+v", serverMove)
		move := Move{nil, &Pass{setupRequest.Punter}}
		err = send(conn, move)
		if err != nil {
			return
		}
	}
	return
}
