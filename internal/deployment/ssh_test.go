package deployment

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type testSSHServer struct {
	host      credential
	authCalls atomic.Int32
}

func newTestSSHServer(t *testing.T, handle func(<-chan ssh.NewChannel)) *testSSHServer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	signer, err := ssh.NewSignerFromKey(key)
	must(t, err)
	server := &testSSHServer{}
	config := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			server.authCalls.Add(1)
			if meta.User() != "test" || string(password) != "secret" {
				return nil, errors.New("invalid credentials")
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(t, err)
	ecdsaSigner, err := ssh.NewSignerFromKey(ecdsaKey)
	must(t, err)
	config.AddHostKey(ecdsaSigner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	server.host = credential{
		Host: Host{
			Address:     "127.0.0.1",
			Port:        listener.Addr().(*net.TCPAddr).Port,
			User:        "test",
			Auth:        "password",
			Fingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		},
		Password: "secret",
	}
	var connections sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			go func() {
				defer connections.Done()
				defer conn.Close()
				stop := context.AfterFunc(t.Context(), func() { conn.Close() })
				defer stop()
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				handle(channels)
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-done
		connections.Wait()
	})
	return server
}

func acceptTestSSHSession(t *testing.T, channels <-chan ssh.NewChannel) (ssh.Channel, <-chan *ssh.Request) {
	t.Helper()
	channel, ok := <-channels
	if !ok {
		return nil, nil
	}
	if channel.ChannelType() != "session" {
		t.Error("unexpected SSH channel", channel.ChannelType())
		return nil, nil
	}
	stream, requests, err := channel.Accept()
	if err != nil {
		t.Error(err)
	}
	return stream, requests
}

func TestSSHTransport(t *testing.T) {
	server := newTestSSHServer(t, func(channels <-chan ssh.NewChannel) {
		stream, requests := acceptTestSSHSession(t, channels)
		if stream == nil {
			return
		}
		defer stream.Close()
		req := <-requests
		var payload struct{ Command string }
		if req == nil || req.Type != "exec" || ssh.Unmarshal(req.Payload, &payload) != nil || payload.Command != "echo test" {
			t.Error("incorrect SSH command")
			return
		}
		_ = req.Reply(true, nil)
		_, _ = io.WriteString(stream, "connected")
		_, _ = stream.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var output tail
	must(t, runSSH(ctx, server.host, "echo test", nil, &output))
	failIf(t, output.String() != "connected", output.String())
}

func TestSSHRejectsUntrustedHostsAndCredentials(t *testing.T) {
	for _, test := range []struct {
		name      string
		change    func(*credential)
		want      string
		authCalls int32
	}{
		{
			"wrong fingerprint",
			func(h *credential) { h.Fingerprint = "SHA256:" + strings.Repeat("a", 43) },
			"host key mismatch",
			0,
		},
		{
			"missing fingerprint",
			func(h *credential) { h.Fingerprint = "" },
			"host key mismatch",
			0,
		},
		{
			"wrong password",
			func(h *credential) { h.Password = "wrong" },
			"authenticate",
			1,
		},
		{
			"missing password",
			func(h *credential) { h.Password = "" },
			"password is required",
			0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newTestSSHServer(t, func(<-chan ssh.NewChannel) { t.Error("unauthorized SSH session") })
			host := server.host
			test.change(&host)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err := runSSH(ctx, host, "must not run", nil, io.Discard)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatal("unexpected SSH result", err)
			}
			failIf(t, server.authCalls.Load() != test.authCalls, "authentication sent before verifying the host")
		})
	}
}

func TestSSHSessionCancellation(t *testing.T) {
	entered := make(chan struct{})
	closed := make(chan struct{})
	server := newTestSSHServer(t, func(channels <-chan ssh.NewChannel) {
		defer close(closed)
		stream, requests := acceptTestSSHSession(t, channels)
		if stream == nil {
			return
		}
		defer stream.Close()
		req := <-requests
		if req == nil {
			return
		}
		_ = req.Reply(true, nil)
		close(entered)
		for range requests {
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- runSSH(ctx, server.host, "block", nil, io.Discard) }()
	deadline, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	select {
	case <-entered:
	case <-deadline.Done():
		t.Fatal("SSH command did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-deadline.Done():
		t.Fatal("SSH ignored cancellation")
	}
	select {
	case <-closed:
	case <-deadline.Done():
		t.Fatal("SSH connection remained open")
	}
}

func TestSSHHandshakeCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	defer listener.Close()
	entered := make(chan struct{})
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		close(entered)
		_, _ = io.Copy(io.Discard, conn)
	}()
	host := credential{
		Host: Host{
			Address:     "127.0.0.1",
			Port:        listener.Addr().(*net.TCPAddr).Port,
			User:        "test",
			Auth:        "password",
			Fingerprint: "SHA256:" + strings.Repeat("a", 43),
		},
		Password: "secret",
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- runSSH(ctx, host, "must not run", nil, io.Discard) }()
	deadline, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	select {
	case <-entered:
	case <-deadline.Done():
		t.Fatal("SSH connection did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-deadline.Done():
		t.Fatal("SSH handshake ignored cancellation")
	}
	select {
	case <-closed:
	case <-deadline.Done():
		t.Fatal("SSH handshake connection remained open")
	}
}

func newTestSSHForwarder(t *testing.T) *testSSHServer {
	t.Helper()
	return newTestSSHServer(t, func(channels <-chan ssh.NewChannel) {
		for channel := range channels {
			var target struct {
				Host       string
				Port       uint32
				Origin     string
				OriginPort uint32
			}
			if channel.ChannelType() != "direct-tcpip" || ssh.Unmarshal(channel.ExtraData(), &target) != nil {
				t.Error("not a forwarded connection")
				return
			}
			if target.Host != "127.0.0.1" {
				t.Errorf("forwarding host = %q, want remote loopback", target.Host)
				return
			}
			conn, err := net.Dial("tcp", net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))))
			if err != nil {
				t.Error(err)
				return
			}
			stream, requests, err := channel.Accept()
			if err != nil {
				conn.Close()
				t.Error(err)
				return
			}
			go ssh.DiscardRequests(requests)
			go func() {
				defer conn.Close()
				defer stream.Close()
				go func() { _, _ = io.Copy(conn, stream) }()
				_, _ = io.Copy(stream, conn)
			}()
		}
	})
}
