package deployment

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

type SSHKey struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}
type storedKey struct {
	SSHKey
	Passphrase string `json:"passphrase,omitempty"`
}

func (m *Manager) hostCredential(h credential) credential {
	h.key = m.db.Keys[h.KeyID]
	return h
}

func (k storedKey) authentication() (ssh.AuthMethod, error) {
	if !filepath.IsAbs(k.Path) {
		return nil, errors.New("key path must be absolute on the RTCBench server")
	}
	f, err := os.Open(k.Path)
	if err != nil {
		return nil, errors.New("cannot open private key file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 32769))
	if err != nil || len(data) > 32768 {
		return nil, errors.New("cannot read private key file (maximum 32 KiB)")
	}
	var signer ssh.Signer
	if k.Passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(k.Passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(data)
	}
	if err != nil {
		return nil, errors.New("invalid private key or passphrase; use a PEM or OpenSSH private key")
	}
	return ssh.PublicKeys(signer), nil
}

func (m *Manager) saveKey(k storedKey, id string) (SSHKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	previous, exists := m.db.Keys[id]
	if id != "" && !exists {
		return SSHKey{}, errors.New("SSH key not found")
	}
	k.Name, k.Path = strings.TrimSpace(k.Name), strings.TrimSpace(k.Path)
	if k.Name == "" || len(k.Name) > 80 {
		return SSHKey{}, errors.New("provide a key name (at most 80 characters)")
	}
	for otherID, other := range m.db.Keys {
		if otherID != id && strings.EqualFold(other.Name, k.Name) {
			return SSHKey{}, errors.New("SSH key name already exists")
		}
	}
	if exists && k.Path == previous.Path && k.Passphrase == "" {
		k.Passphrase = previous.Passphrase
	}
	if _, err := k.authentication(); err != nil {
		return SSHKey{}, err
	}
	if id == "" {
		id = uuid.NewString()
	}
	k.ID = id
	return k.SSHKey, saveRecord(m, m.db.Keys, id, k)
}
