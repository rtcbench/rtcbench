package deployment

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type Host struct {
	KeyID       string      `json:"keyId,omitempty"`
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Address     string      `json:"address"`
	Port        int         `json:"port"`
	User        string      `json:"user"`
	Fingerprint string      `json:"fingerprint"`
	Auth        string      `json:"auth"`
	Sudo        bool        `json:"sudo"`
	Networks    []Interface `json:"networks,omitempty"`
}

type credential struct {
	Host
	Password string `json:"password,omitempty"`
	key      storedKey
}

var hostname = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}$`)
var fingerprint = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

func (h *credential) validate() error {
	h.Name = strings.TrimSpace(h.Name)
	h.Address = strings.ToLower(strings.TrimSpace(h.Address))
	h.User = strings.TrimSpace(h.User)
	h.Fingerprint = strings.TrimSpace(h.Fingerprint)
	if h.Port == 0 {
		h.Port = 22
	}
	if h.Name == "" || len(h.Name) > 80 || (!hostname.MatchString(h.Address) && net.ParseIP(h.Address) == nil) || h.Port < 1 || h.Port > 65535 || h.User == "" || len(h.User) > 64 || strings.ContainsAny(h.User, "\r\n\x00") {
		return errors.New("provide a name, SSH hostname or IP, port (1–65535), and username")
	}
	if !fingerprint.MatchString(h.Fingerprint) {
		return errors.New("provide the server's SHA256 SSH host-key fingerprint")
	}
	_, err := h.authentication()
	return err
}

func (h credential) authentication() (ssh.AuthMethod, error) {
	switch h.Auth {
	case "password":
		if h.Password == "" {
			return nil, errors.New("password is required")
		}
		return ssh.Password(h.Password), nil
	case "file":
		return h.key.authentication()
	default:
		return nil, errors.New("choose key or password authentication")
	}
}

func (h credential) checkKey(_ string, _ net.Addr, key ssh.PublicKey) error {
	actual := ssh.FingerprintSHA256(key)
	if subtle.ConstantTimeCompare([]byte(actual), []byte(h.Fingerprint)) != 1 {
		return fmt.Errorf("SSH host key mismatch: received %s; verify the fingerprint with your server administrator", actual)
	}
	return nil
}

type remoteFunc func(context.Context, credential, string, io.Reader, io.Writer) error

func runSSH(ctx context.Context, h credential, command string, input io.Reader, output io.Writer) error {
	return withSSH(ctx, h, func(session *ssh.Session) error {
		session.Stdin, session.Stdout, session.Stderr = input, output, output
		return session.Run(command)
	})
}

func withSSH(ctx context.Context, h credential, run func(*ssh.Session) error) error {
	return withSSHClient(ctx, h, func(client *ssh.Client) error {
		session, err := client.NewSession()
		if err != nil {
			return err
		}
		defer session.Close()
		return run(session)
	})
}

func withSSHClient(ctx context.Context, h credential, run func(*ssh.Client) error) (err error) {
	defer func() {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	auth, err := h.authentication()
	if err != nil {
		return err
	}
	address := net.JoinHostPort(h.Address, strconv.Itoa(h.Port))
	conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	c, channels, requests, err := ssh.NewClientConn(conn, address, &ssh.ClientConfig{
		User:            h.User,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: h.checkKey,
		// Prefer the Ed25519 host key shown in the setup instructions
		HostKeyAlgorithms: []string{
			ssh.KeyAlgoED25519,
			ssh.KeyAlgoECDSA256,
			ssh.KeyAlgoECDSA384,
			ssh.KeyAlgoECDSA521,
			ssh.KeyAlgoRSASHA512,
			ssh.KeyAlgoRSASHA256,
		},
	})
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(c, channels, requests)
	defer client.Close()
	return run(client)
}
