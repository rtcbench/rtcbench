package deployment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

type Deployment struct {
	Overrides map[string]string `json:"overrides,omitempty"`
	Ports     map[string]int    `json:"ports"`
	Name      string            `json:"name"`
	ID        string            `json:"id"`
	NetworkID string            `json:"networkId,omitempty"`
	HostID    string            `json:"hostId,omitempty"`
	Plugin    string            `json:"plugin"`
	Address   string            `json:"address"`
	Status    string            `json:"status"`
	Operation string            `json:"operation,omitempty"`
	Progress  string            `json:"progress"`
	Error     string            `json:"error,omitempty"`
	Logs      string            `json:"logs"`
	CreatedAt time.Time         `json:"createdAt"`
	CheckedAt *time.Time        `json:"checkedAt,omitempty"`
}

type deployment struct {
	Deployment
	Secret string `json:"secret"`
	APIKey string `json:"apiKey,omitempty"`
}

func (d deployment) config() deploy.Config {
	return deploy.Config{
		ID:      d.ID,
		Plugin:  d.Plugin,
		Address: d.Address,
		Ports:   d.Ports,
		APIKey:  d.APIKey,
		Secret:  d.Secret,
	}
}

type database struct {
	Keys        map[string]storedKey  `json:"keys"`
	Networks    map[string]Network    `json:"networks"`
	Hosts       map[string]credential `json:"hosts"`
	Deployments map[string]deployment `json:"deployments"`
}

type Manager struct {
	LoadBuildContext func(context.Context, string) (map[string]string, error)
	plugins          map[string]deploy.Plugin
	pluginList       []deploy.Plugin
	mu               sync.Mutex
	db               database
	path             string
	remote           remoteFunc
	ctx              context.Context
	cancel           context.CancelFunc
	jobs             sync.WaitGroup
	closed           bool
}

func New(dir string, definitions ...deploy.Plugin) (*Manager, error) {
	plugins, err := registerPlugins(definitions)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := loadDatabase(filepath.Join(dir, "state.json"), plugins)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		path:       filepath.Join(dir, "state.json"),
		plugins:    plugins,
		pluginList: append([]deploy.Plugin{}, definitions...),
		remote:     runSSH,
		ctx:        ctx,
		cancel:     cancel,
		db:         db,
	}, nil
}

func loadDatabase(path string, plugins map[string]deploy.Plugin) (database, error) {
	db := database{
		Keys:        map[string]storedKey{},
		Networks:    map[string]Network{},
		Hosts:       map[string]credential{},
		Deployments: map[string]deployment{},
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return db, nil
	}
	if err != nil {
		return db, err
	}
	if err := json.Unmarshal(data, &db); err != nil {
		return db, err
	}
	if db.Keys == nil || db.Networks == nil || db.Hosts == nil || db.Deployments == nil {
		return db, errors.New("invalid systems state file")
	}
	for id, d := range db.Deployments {
		if _, err := uuid.Parse(id); err != nil || id != d.ID {
			return db, errors.New("invalid deployment ID in systems state")
		}
		d.Ports, err = resolvePorts(plugins, d.Plugin, d.Ports, d.HostID != "")
		if err != nil {
			return db, err
		}
		if d.HostID != "" && d.Status != "stopped" {
			d.Status = "unknown"
			d.Operation = ""
			d.Progress = "App restarted; check health or stop before redeploying."
		}
		db.Deployments[id] = d
	}
	return db, nil
}

func (m *Manager) Close(ctx context.Context) {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.jobs.Wait()
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Caller must hold m.mu
func (m *Manager) save() error {
	data, err := json.Marshal(m.db)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), m.path)
}

// Caller must hold m.mu
func saveRecord[T any](m *Manager, records map[string]T, id string, value T) error {
	previous, exists := records[id]
	records[id] = value
	if err := m.save(); err != nil {
		if exists {
			records[id] = previous
		} else {
			delete(records, id)
		}
		return err
	}
	return nil
}

func (m *Manager) saveHost(h credential, id string) (Host, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkInterfaces(h.Networks); err != nil {
		return Host{}, err
	}
	previous, exists := m.db.Hosts[id]
	if id != "" && !exists {
		return Host{}, errors.New("server not found")
	}
	h, err := m.prepareHost(h, previous)
	if err != nil {
		return Host{}, err
	}
	addressChanged := h.Address != previous.Address || h.Port != previous.Port || h.User != previous.User
	if exists && addressChanged && m.hostOccupied(id, "") {
		return Host{}, errors.New("stop this server's deployment before changing its SSH address, port or username; credentials can be replaced now")
	}
	for otherID, other := range m.db.Hosts {
		if otherID != id && other.Address == h.Address && other.Port == h.Port {
			return Host{}, errors.New("this SSH server is already saved")
		}
	}
	if id == "" {
		id = uuid.NewString()
	}
	h.ID = id
	return h.Host, saveRecord(m, m.db.Hosts, id, h)
}

func (m *Manager) prepareHost(h, previous credential) (credential, error) {
	if h.Auth == "password" && previous.Auth == "password" && h.Password == "" {
		h.Password = previous.Password
	}
	if h.Auth == "file" {
		if _, ok := m.db.Keys[h.KeyID]; !ok {
			return h, errors.New("select a saved SSH key")
		}
		h.Password = ""
	} else {
		h.KeyID = ""
	}
	resolved := m.hostCredential(h)
	if err := resolved.validate(); err != nil {
		return h, err
	}
	h.Host = resolved.Host
	return h, nil
}

func (m *Manager) checkInterfaces(interfaces []Interface) error {
	seen := map[string]bool{}
	for _, item := range interfaces {
		if err := m.checkAddress(item); err != nil {
			return err
		}
		if seen[item.NetworkID] {
			return errors.New("select each network once")
		}
		seen[item.NetworkID] = true
	}
	return nil
}

func (m *Manager) hostOccupied(host, id string) bool {
	for otherID, d := range m.db.Deployments {
		if otherID != id && d.HostID == host && d.Status != "stopped" {
			return true
		}
	}
	return false
}

type Start struct {
	Ports     map[string]int `json:"ports"`
	Name      string         `json:"name"`
	HostID    string         `json:"hostId"`
	Plugin    string         `json:"plugin"`
	Address   string         `json:"address"`
	NetworkID string         `json:"networkId"`
	APIKey    string         `json:"apiKey"`
	Secret    string         `json:"secret"`
}

func (m *Manager) start(req Start) (Deployment, error) {
	d, err := req.deployment(m.plugins)
	if err != nil {
		return Deployment{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Deployment{}, errors.New("server is shutting down")
	}
	if err := m.checkDeployment(d); err != nil {
		return Deployment{}, err
	}
	if err := saveRecord(m, m.db.Deployments, d.ID, d); err != nil {
		return Deployment{}, err
	}
	if d.HostID != "" {
		m.launch(d, "deploy")
	}
	return d.Deployment, nil
}

func (req Start) deployment(plugins map[string]deploy.Plugin) (deployment, error) {
	name := strings.TrimSpace(req.Name)
	if len(name) > 80 {
		return deployment{}, errors.New("deployment name must be at most 80 characters")
	}
	if name == "" {
		name = req.Plugin + " | " + req.Address
	}
	ip := net.ParseIP(strings.TrimSpace(req.Address))
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return deployment{}, errors.New("choose a supported plugin and a reachable IPv4 address for signaling and media")
	}
	ports, err := resolvePorts(plugins, req.Plugin, req.Ports, req.HostID != "")
	if err != nil {
		return deployment{}, err
	}
	d := deployment{
		Deployment: Deployment{
			ID:        uuid.NewString(),
			Name:      name,
			HostID:    req.HostID,
			NetworkID: req.NetworkID,
			Plugin:    req.Plugin,
			Address:   ip.String(),
			Ports:     ports,
			Status:    "configured",
			CreatedAt: time.Now(),
		},
		APIKey: req.APIKey,
		Secret: req.Secret,
	}
	if req.HostID == "" {
		if plugins[req.Plugin].Credentials && (req.APIKey == "" || req.Secret == "") {
			return d, errors.New("API key and secret are required")
		}
		return d, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return d, err
	}
	d.Secret = hex.EncodeToString(secret)
	d.APIKey = "rtcbench"
	d.Status = "deploying"
	d.Operation = "deploy"
	d.Progress = "Connecting to SSH server"
	return d, nil
}

func (m *Manager) checkDeployment(d deployment) error {
	if d.HostID != "" {
		if _, ok := m.db.Hosts[d.HostID]; !ok {
			return errors.New("select a saved SSH server")
		}
		if m.hostOccupied(d.HostID, "") {
			return errors.New("stop the existing deployment first; SFU media ports overlap")
		}
	}
	if d.NetworkID != "" {
		return m.checkAddress(Interface{d.NetworkID, d.Address})
	}
	return nil
}

func (m *Manager) action(id, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.db.Deployments[id]
	if !ok {
		return errors.New("deployment not found")
	}
	if d.HostID == "" {
		return errors.New("this deployment is managed elsewhere")
	}
	if m.closed {
		return errors.New("server is shutting down")
	}
	if d.Operation != "" {
		return errors.New("an operation is already in progress")
	}
	if action == "retry" && d.Status != "stopped" {
		return errors.New("stop the previous attempt before retrying")
	}
	if action == "retry" && m.hostOccupied(d.HostID, d.ID) {
		return errors.New("another deployment occupies this server")
	}
	if action == "retry" {
		action = "deploy"
	}
	if _, ok := operations[action]; !ok {
		return errors.New("unknown deployment action")
	}
	d.Operation = action
	if action != "logs" {
		d.Error, d.Progress = "", "Connecting to SSH server"
	}
	if action == "deploy" {
		d.Status = "deploying"
	}
	if action == "stop" {
		d.Status = "stopping"
	}
	if err := saveRecord(m, m.db.Deployments, id, d); err != nil {
		return err
	}
	m.launch(d, action)
	return nil
}

func (m *Manager) phase(id, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.db.Deployments[id]
	d.Progress = text
	m.db.Deployments[id] = d
}

// SSH writes stdout and stderr concurrently
type tail struct {
	mu   sync.Mutex
	data []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	const limit = 64 << 10
	if len(p) > limit {
		p = p[len(p)-limit:]
	}
	t.data = append(t.data, p...)
	if len(t.data) > limit {
		t.data = t.data[len(t.data)-limit:]
	}
	return n, nil
}
func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.data)
}

func redact(value string, h credential, d deployment) string {
	for _, secret := range []string{h.Password, h.key.Passphrase, d.Secret} {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}

func preflight(h credential) string {
	return `test "$(uname -s)" = Linux || { echo 'Linux is required' >&2; exit 1; }
command -v tar >/dev/null || { echo 'tar is required' >&2; exit 1; }
command -v docker >/dev/null || { echo 'Install Docker Engine and Docker Compose v2 on this server' >&2; exit 1; }
` + docker(h) + " info --format '{{.OSType}}' && " + docker(h) + " compose version"
}

func (m *Manager) snapshot() any {
	m.mu.Lock()
	defer m.mu.Unlock()
	hosts := []Host{}
	deployments := []Deployment{}
	for _, h := range m.db.Hosts {
		hosts = append(hosts, h.Host)
	}
	for _, d := range m.db.Deployments {
		view := d.Deployment
		view.Overrides = settings(m.plugins[d.Plugin], d)
		deployments = append(deployments, view)
	}
	slices.SortFunc(hosts, func(a, b Host) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(deployments, func(a, b Deployment) int { return b.CreatedAt.Compare(a.CreatedAt) })
	networks := []Network{}
	keys := []SSHKey{}
	for _, k := range m.db.Keys {
		keys = append(keys, k.SSHKey)
	}
	slices.SortFunc(keys, func(a, b SSHKey) int { return strings.Compare(a.Name, b.Name) })
	for _, n := range m.db.Networks {
		networks = append(networks, n)
	}
	slices.SortFunc(networks, func(a, b Network) int { return strings.Compare(a.Name, b.Name) })
	return object{"hosts": hosts, "deployments": deployments, "plugins": m.pluginList, "networks": networks, "keys": keys}
}
