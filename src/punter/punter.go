package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	logging "log"
	"net"
	"os"
	"syscall"
)

var log *logging.Logger = logging.New(os.Stderr, "bi: ", 0)

type readWriter struct {
	io.Reader
	io.Writer
}

func NewReadWriter(r io.Reader, w io.Writer) io.ReadWriter {
	return &readWriter{r, w}
}

// Flags are only used for online mode
var onlineMode = flag.Bool("online", false, "Use online mode")
var server = flag.String("server", "punter.inf.ed.ac.uk", "server ip")
var port = flag.Int("port", 9001, "server port")
var name = flag.String("name", "blueiris", "bot name")

type HandshakeRequest struct {
	Me string `json:"me"`
}

type HandshakeResponse struct {
	You string `json:"you"`
}

type PunterID uint
type SiteID uint

// Offset of river in Map.Rivers
type RiverOffset int
type RiverOffsets []RiverOffset

// Offset of site in Map.Sites
type SiteOffset int

type ScoreValue int

type Site struct {
	ID     SiteID       `json:"id"`
	Rivers RiverOffsets `json:"rivers,omitempty"`
}

type River struct {
	Source  SiteID   `json:"source"`
	Target  SiteID   `json:"target"`
	Claimed bool     `json:"claimed,omitempty"`
	Owner   PunterID `json:"owner,omitempty"`
	// Use for searching
	Flags int `json:"flags,omitempty"`
}

type Map struct {
	Sites  []Site   `json:"sites"`
	Rivers []River  `json:"rivers"`
	Mines  []SiteID `json:"mines"`
	// Lookup site from siteID
	SiteMap map[SiteID]SiteOffset `json:"siteMap,omitempty"`
}

func (m *Map) DecorateMap() (err error) {
	m.SiteMap = make(map[SiteID]SiteOffset)
	for i, site := range m.Sites {
		m.SiteMap[site.ID] = SiteOffset(i)
	}
	for i, river := range m.Rivers {
		{
			sourceIndex := m.SiteMap[river.Source]
			m.Sites[sourceIndex].Rivers = append(m.Sites[sourceIndex].Rivers, RiverOffset(i))
		}
		{
			targetIndex := m.SiteMap[river.Target]
			m.Sites[targetIndex].Rivers = append(m.Sites[targetIndex].Rivers, RiverOffset(i))
		}
	}
	return
}

type SetupRequest struct {
	Punter  PunterID `json:"punter"`
	Punters int      `json:"punters"`
	Map     Map      `json:"map"`
}

type State struct {
	Punter  PunterID `json:"punter"`
	Punters int      `json:"punters"`
	Map     Map      `json:"map"`
}

type SetupResponse struct {
	Ready PunterID `json:"ready"`
	State *State   `json:"state,omitempty"`
}

type Claim struct {
	Punter PunterID `json:"punter"`
	Source SiteID   `json:"source"`
	Target SiteID   `json:"target"`
}

type Pass struct {
	Punter PunterID `json:"punter"`
}

// Poor man's union type. Only one of Claim or Pass is non-nil
type Move struct {
	Claim *Claim `json:"claim,omitempty"`
	Pass  *Pass  `json:"pass,omitempty"`
	State *State `json:"state,omitempty"`
}

func (m Move) String() string {
	if m.Claim != nil {
		return fmt.Sprintf("claim:%+v", m.Claim)
	} else if m.Pass != nil {
		return fmt.Sprintf("pass:%+v", m.Pass)
	} else {
		return "empty"
	}
}

type Moves struct {
	Moves []Move `json:"moves"`
}

type Score struct {
	Punter PunterID   `json:"punter"`
	Score  ScoreValue `json:"score"`
}

type Stop struct {
	Moves  []Move  `json:"moves"`
	Scores []Score `json:"scores"`
}

// Poor man's union. Only one of Move or Stop is non-nil
type ServerMove struct {
	Move  *Moves `json:"move",omitempty`
	Stop  *Stop  `json:"stop",omitempty`
	State *State `json:"state",omitempty`
}

func findServer() (conn net.Conn, err error) {
	p := *port
	serverAddress := fmt.Sprintf("%s:%d", *server, p)
	// log.Printf("Trying %s", serverAddress)
	conn, err = net.Dial("tcp", serverAddress)
	if err == nil {
		return
	}
	log.Fatal()
	return
}

func send(writer io.Writer, d interface{}) (err error) {
	var b []byte
	buf := bytes.NewBuffer(nil)
	err = json.NewEncoder(buf).Encode(d)
	if err != nil {
		return
	}
	b = buf.Bytes()
	// Don't need to send linefeed at end
	b = b[:len(b)-1]
	msg := fmt.Sprintf("%d:%s", len(b), b)
	// log.Printf("Sending: %s", msg)
	_, err = io.WriteString(writer, msg)
	// log.Printf("sent %d bytes", n)
	if err != nil {
		return
	}
	return err
}

func receiveRaw(reader io.Reader) (b1 []byte, err error) {
	var i int
	_, err = fmt.Fscanf(reader, "%d:", &i)
	if err != nil {
		return
	}
	// log.Printf("Reading %d bytes", i)
	b1 = make([]byte, i)
	offset := 0
	for offset < i {
		var n int
		n, err = reader.Read(b1[offset:])
		if err != nil {
			return
		}
		offset += n
	}
	// log.Printf("Bytes: %d %s", len(b1), string(b1))
	// listen for reply
	return
}

func receive(conn io.Reader, d interface{}) (err error) {
	var b1 []byte
	b1, err = receiveRaw(conn)
	if err != nil {
		return
	}
	// log.Printf("Received Bytes: %d %s", len(b1), string(b1))
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
	// log.Printf("response %v\n", handshakeResponse)
	if err != nil {
		return
	}
	return
}

func setup(conn io.ReadWriter) (state *State, err error) {
	var setupRequest SetupRequest
	err = receive(conn, &setupRequest)
	if err != nil {
		return
	}
	if pretty, err2 := json.MarshalIndent(&setupRequest, "", " "); err2 == nil {
		log.Printf("Received setupRequest %s", pretty)
	}
	state, err = doSetup(conn, setupRequest)
	return
}

func doSetup(writer io.Writer, setupRequest SetupRequest) (state *State, err error) {
	state = &State{}
	state.Punter = setupRequest.Punter
	state.Punters = setupRequest.Punters
	state.Map = setupRequest.Map
	state.Map.DecorateMap()
	setupResponse := SetupResponse{setupRequest.Punter, nil}
	if !*onlineMode {
		setupResponse.State = state
	}
	err = send(writer, &setupResponse)
	return
}

func processServerMove(conn io.ReadWriter, state *State, serverMove ServerMove) (err error) {
	if serverMove.Move != nil {
		return doMoves(conn, state, *serverMove.Move)
	} else if serverMove.Stop != nil {
		return doStop(state, *serverMove.Stop)
	} else {
		return
	}
}

func doMoves(conn io.ReadWriter, state *State, moves Moves) (err error) {
	err = processServerMoves(conn, state, moves)
	if err != nil {
		return
	}
	err = pickMove(conn, state)
	if err != nil {
		return
	}
	return
}

type RiverEdge struct {
	Source SiteID
	Target SiteID
}

func (state *State) SlowRiverToRiverIndex(edge RiverEdge) (ro RiverOffset) {
	ro = RiverOffset(-1)
	for riverIndex, river := range state.Map.Rivers {
		if edge.Source == river.Source && edge.Target == river.Target {
			ro = RiverOffset(riverIndex)
			break
		}
	}
	return
}

func (state *State) RiverToRiverOffset(edge RiverEdge) (ro RiverOffset) {
	sourceOffset := state.Map.SiteMap[edge.Source]
	sourceRivers := state.Map.Sites[sourceOffset].Rivers
	targetOffset := state.Map.SiteMap[edge.Target]
	targetRivers := state.Map.Sites[targetOffset].Rivers
	var shorterRivers RiverOffsets
	if len(sourceRivers) < len(targetRivers) {
		shorterRivers = sourceRivers
	} else {
		shorterRivers = targetRivers
	}
	ro = RiverOffset(-1)
	for _, riverOffset := range shorterRivers {
		river := state.Map.Rivers[riverOffset]
		if edge.Source == river.Source && edge.Target == river.Target {
			ro = riverOffset
			break
		}
	}
	return
}

func processServerMoves(conn io.ReadWriter, state *State, moves Moves) (err error) {
	for _, move := range moves.Moves {
		if move.Claim != nil {
			edge := RiverEdge{move.Claim.Source, move.Claim.Target}
			riverOffset := state.RiverToRiverOffset(edge)
			state.Map.Rivers[riverOffset].Claimed = true
			state.Map.Rivers[riverOffset].Owner = move.Claim.Punter
		}
	}
	return
}

func pickMove(conn io.ReadWriter, state *State) (err error) {
	var move Move
	move, err = pickExtendRoute(state)
	if err != nil {
		return
	}
	if !*onlineMode {
		move.State = state
	}
	log.Printf("Move: %v", move)
	err = send(conn, move)
	if err != nil {
		return
	}
	return
}

func doStop(state *State, stop Stop) (err error) {
	log.Printf("I am punter %d", state.Punter)
	for _, score := range stop.Scores {
		log.Printf("Punter: %d score: %d", score.Punter, score.Score)
	}
	return
}

func pickPass(state *State) (move Move, err error) {
	move.Pass = &Pass{state.Punter}
	return
}

func pickFirstUnclaimed(state *State) (move Move, err error) {
	for _, river := range state.Map.Rivers {
		if river.Claimed == false {
			move.Claim = &Claim{state.Punter, river.Source, river.Target}
			return
		}
	}
	return pickPass(state)
}

func (s *State) ClaimRiverOffset(riverOffset RiverOffset, owner PunterID) {
	s.Map.Rivers[riverOffset].Claimed = true
	s.Map.Rivers[riverOffset].Owner = owner
}

func (s *State) UnclaimRiverOffset(riverOffset RiverOffset) {
	s.Map.Rivers[riverOffset].Claimed = false
	s.Map.Rivers[riverOffset].Owner = PunterID(0)
}

type RiverScore struct {
	ScoreValue  ScoreValue
	RiverOffset RiverOffset
}

func pickExtendRoute(state *State) (move Move, err error) {
	best := RiverScore{ScoreValue(-1), RiverOffset(-1)}
	for _, mine := range state.Map.Mines {
		mineSiteOffset := state.Map.SiteMap[mine]
		localScore := bestMove(state, mineSiteOffset, mineSiteOffset)
		if localScore.ScoreValue > best.ScoreValue {
			best = localScore
		}
	}
	if best.ScoreValue > 0 {
		river := state.Map.Rivers[best.RiverOffset]
		move.Claim = &Claim{state.Punter, river.Source, river.Target}
		return
	}
	log.Printf("Couldn't pick extended route.")
	return pickFirstUnclaimed(state)
}

func bestMove(state *State, mineSiteOffset SiteOffset, currentOffset SiteOffset) (best RiverScore) {
	for _, riverOffset := range state.Map.Sites[currentOffset].Rivers {
		river := state.Map.Rivers[riverOffset]
		var localBest RiverScore
		if river.Claimed == false {
			localBest.ScoreValue = 1
			localBest.RiverOffset = riverOffset
		} else if river.Owner == state.Punter {
			if river.Flags == 0 {
				state.Map.Rivers[riverOffset].Flags = 1
				sourceOffset := state.Map.SiteMap[river.Source]
				targetOffset := state.Map.SiteMap[river.Target]
				var otherEndOffset SiteOffset
				if sourceOffset == currentOffset {
					otherEndOffset = targetOffset
				} else if targetOffset == currentOffset {
					otherEndOffset = sourceOffset
				} else {
					panic("Unknown offset")
				}
				localBest := bestMove(state, mineSiteOffset, otherEndOffset)
				if localBest.ScoreValue > 0 {
					// Bonus for deeper chains
					localBest.ScoreValue += 1
				}
				state.Map.Rivers[riverOffset].Flags = 0
			}
		}
		if localBest.ScoreValue > best.ScoreValue {
			best = localBest
		}
	}
	return
}

func runOnlineMode() (err error) {
	conn, err := findServer()
	if err != nil {
		return
	}
	// log.Printf("connected")
	err = handshake(conn)
	if err != nil {
		return
	}

	// log.Printf("setup")

	var state *State
	state, err = setup(conn)
	if err != nil {
		return
	}

	// log.Printf("game")
	for {
		var serverMove ServerMove
		err = receive(conn, &serverMove)
		if err != nil {
			return
		}
		err = processServerMove(conn, state, serverMove)
		if err != nil {
			return
		}
	}
	return
}

func runOfflineMode() (err error) {
	conn := NewReadWriter(os.Stdin, os.Stdout)
	// log.Printf("connected")
	err = handshake(conn)
	if err != nil {
		return
	}

	var b1 []byte
	b1, err = receiveRaw(conn)
	if err != nil {
		return
	}
	var serverRequest map[string]interface{}
	err = json.Unmarshal(b1, &serverRequest)
	if err != nil {
		return
	}

	if serverRequest["punter"] != nil {
		// log.Printf("setup")
		var setupRequest SetupRequest
		err = json.Unmarshal(b1, &setupRequest)
		if err != nil {
			return
		}
		_, err = doSetup(conn, setupRequest)
		return
	} else if serverRequest["move"] != nil {
		// log.Printf("move")
		var serverMove ServerMove
		err = json.Unmarshal(b1, &serverMove)
		if err != nil {
			return
		}
		return doMoves(conn, serverMove.State, *serverMove.Move)
	} else if serverRequest["stop"] != nil {
		// log.Printf("stop")
		var serverMove ServerMove
		err = json.Unmarshal(b1, &serverMove)
		if err != nil {
			return
		}
		return doStop(serverMove.State, *serverMove.Stop)
	} else {
		err = fmt.Errorf("Unknown server request %s", b1)
	}
	return
}

// This is needed when running under lamduct on VM. Otherwise
// EAGAIN.
func fixIO() {
	fd := int(os.Stdin.Fd())
	syscall.SetNonblock(fd, false)
}

func main() {
	var err error
	flag.Parse()
	fixIO()
	if *onlineMode {
		// log.Printf("online mode")
		err = runOnlineMode()
	} else {
		// log.Printf("offline mode")
		err = runOfflineMode()
	}
	if err != nil {
		log.Fatal(err)
	}
}
