package setup

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"testing"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// A stand-in for Nginx on port 80: forwards the challenge path to the
// solver port, serves a site for everything else.
func TestVerifyChallengeForwarding(t *testing.T) {
	solver := freePort(t)
	target, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(solver))
	mux := http.NewServeMux()
	mux.Handle("/.well-known/acme-challenge/", httputil.NewSingleHostReverseProxy(target))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("site")) })
	proxyLn, _ := net.Listen("tcp", "127.0.0.1:0")
	go http.Serve(proxyLn, mux)
	defer proxyLn.Close()
	domain := proxyLn.Addr().String() // host:port stands in for the domain

	if err := VerifyChallengeForwarding(context.Background(), domain, solver); err != nil {
		t.Fatalf("forwarding configured, but: %v", err)
	}
	if err := VerifyChallengeForwarding(context.Background(), domain, freePort(t)); err == nil {
		t.Fatal("forwarding to the wrong port was reported as working")
	}
}

func TestListeningPortsSeesOwnListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	tcp, _ := ListeningPorts()
	if !tcp[ln.Addr().(*net.TCPAddr).Port] {
		t.Fatal("listening port not found in /proc/net/tcp")
	}
}

func TestFirstFree(t *testing.T) {
	if p := FirstFree(map[int]bool{443: true}, 443, 8443); p != 8443 {
		t.Fatalf("got %d", p)
	}
	if p := FirstFree(map[int]bool{443: true}, 443); p != 0 {
		t.Fatalf("got %d", p)
	}
}
