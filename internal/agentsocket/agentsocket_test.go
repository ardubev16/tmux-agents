package agentsocket

import (
	"bufio"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// serve answers each connection with handle, like integrations/omp.ts.
func serve(t *testing.T, handle func(request string, conn net.Conn)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, _ := bufio.NewReader(conn).ReadString('\n')
				handle(line, conn)
			}()
		}
	}()
	return path
}

func TestRequestSendsOneLineAndDecodesTheReply(t *testing.T) {
	path := serve(t, func(request string, conn net.Conn) {
		if request != `{"op":"get_draft"}`+"\n" {
			conn.Write([]byte(`{"error":"unexpected request"}` + "\n"))
			return
		}
		conn.Write([]byte(`{"draft":"half typed"}` + "\n"))
	})
	var reply struct{ Draft string }
	if err := Request(path, map[string]string{"op": "get_draft"}, &reply); err != nil || reply.Draft != "half typed" {
		t.Errorf("reply %+v, err %v", reply, err)
	}
	if err := Request(filepath.Join(t.TempDir(), "gone.sock"), map[string]string{}, &reply); err == nil {
		t.Error("a missing socket should fail")
	}
	closing := serve(t, func(string, net.Conn) {}) // hangs up without a reply
	if err := Request(closing, map[string]string{}, &reply); err == nil {
		t.Error("no reply should fail")
	}
}

func TestSubscribeStreamsUntilTheAgentHangsUp(t *testing.T) {
	path := serve(t, func(request string, conn net.Conn) {
		conn.Write([]byte(`{"text":"Hi","streaming":true}` + "\n" + `{"text":"Hello","streaming":true}` + "\n"))
	})
	var got []string
	if err := Subscribe(path, make(chan struct{}), func(live Live) { got = append(got, live.Text) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1] != "Hello" {
		t.Errorf("got %q", got)
	}
}

func TestSubscribeEndsWhenStopped(t *testing.T) {
	path := serve(t, func(request string, conn net.Conn) {
		conn.Write([]byte(`{"text":"x"}` + "\n"))
		time.Sleep(time.Minute) // a live agent never hangs up on its own
	})
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		Subscribe(path, stop, func(Live) { close(stop) }) // stop after the first snapshot
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Subscribe kept running after stop was closed")
	}
}
