// Package agentsocket is the client for the socket an agent integration serves (omp: integrations/omp.ts).
//
// Newline-delimited JSON. Requests: {"op": "get_draft"} -> {"draft": str},
// {"op": "set_draft", "text": str} -> {"ok": true}, {"op": "subscribe"} -> a stream of Live snapshots.
package agentsocket

import (
	"bufio"
	"encoding/json"
	"net"
	"time"
)

type Live struct {
	Text      string   `json:"text"`
	Thinking  bool     `json:"thinking"`
	Thoughts  string   `json:"thoughts"` // the streaming message's thinking so far
	Tool      string   `json:"tool"`
	Streaming bool     `json:"streaming"`
	Queued    []string `json:"queued"` // prompts submitted while the agent runs, not delivered yet
}

func newScanner(conn net.Conn) *bufio.Scanner {
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(nil, 16<<20) // live snapshots carry the whole streaming message
	return scanner
}

// Request sends one request and decodes the single-line reply into reply.
func Request(path string, request, reply any) error {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second)) // cannot fail on a fresh unix socket
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return err
	}
	scanner := newScanner(conn)
	if !scanner.Scan() {
		if scanner.Err() != nil {
			return scanner.Err()
		}
		return net.ErrClosed
	}
	return json.Unmarshal(scanner.Bytes(), reply)
}

// Subscribe streams live snapshots to send until the connection ends or stop is closed.
func Subscribe(path string, stop <-chan struct{}, send func(Live)) error {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return err
	}
	go func() { <-stop; conn.Close() }()
	if _, err := conn.Write([]byte(`{"op":"subscribe"}` + "\n")); err != nil {
		return err
	}
	scanner := newScanner(conn)
	for scanner.Scan() {
		var live Live
		if err := json.Unmarshal(scanner.Bytes(), &live); err != nil {
			return err
		}
		send(live)
	}
	return scanner.Err()
}
