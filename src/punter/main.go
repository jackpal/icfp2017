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
	log.Printf("Sending: %s", msg)
	_, err = conn.Write([]byte(msg))
	if err != nil {
		return
	}
	return err
}

func main() {
	flag.Parse()
	conn, err := findServer()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Got connection")
	handshakeRequest := HandshakeRequest{*name}
	err = send(conn, &handshakeRequest)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Waiting for reply")

	var i int
	_, err = fmt.Fscanf(conn, "%d", &i)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("N bytes: %d", i)
	b1 := make([]byte, i)
	_, err = conn.Read(b1)
	if err != nil {
		log.Fatal(err)
	}
	// listen for reply
	var handshakeResponse HandshakeResponse
	if err := json.Unmarshal(b1, &handshakeResponse); err == io.EOF {
		log.Printf("server closed connection")
		if err != nil {
			log.Fatal(err)
		}
		return
	} else if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("response %v\n", handshakeResponse)
}
