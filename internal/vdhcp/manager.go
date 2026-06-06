package vdhcp

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/netutil"
)

type Lease struct {
	RoomID       int64     `json:"roomID"`
	UserID       int64     `json:"userID"`
	IP           string    `json:"ip"`
	SubnetMask   string    `json:"subnetMask"`
	Gateway      string    `json:"gateway"`
	DNS          []string  `json:"dns"`
	MTU          int       `json:"mtu"`
	LeaseSeconds int64     `json:"leaseSeconds"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Manager is an in-memory vDHCP allocator. It keeps leases stable per
// room/user while the server process is alive; durable leases can be added
// later behind this boundary without changing the overlay protocol.
type Manager struct {
	mu    sync.Mutex
	rooms map[int64]*roomPool
}

// roomPool tracks a single room subnet. The gateway uses the last usable
// address, so a /24 room gets .254. Player leases start from the first usable
// address and skip the gateway when the pool wraps.
type roomPool struct {
	roomID      int64
	network     uint32
	broadcast   uint32
	next        uint32
	gateway     uint32
	mask        string
	dns         []string
	mtu         int
	byUser      map[int64]Lease
	ownerByIP   map[uint32]int64
	leaseTTL    time.Duration
	networkCIDR string
}

func NewManager() *Manager {
	return &Manager{rooms: make(map[int64]*roomPool)}
}

// Acquire returns the existing lease for a room/user pair or allocates the next
// available IPv4 address from the room subnet. It is safe for concurrent overlay
// sessions because all pool mutation is protected by Manager.mu.
func (m *Manager) Acquire(roomID, userID int64, networkCIDR string, dns []string, mtu int) (Lease, error) {
	if roomID <= 0 || userID <= 0 {
		return Lease{}, fmt.Errorf("roomID/userID must be positive")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	pool, err := m.ensurePoolLocked(roomID, networkCIDR, dns, mtu)
	if err != nil {
		return Lease{}, err
	}
	if lease, ok := pool.byUser[userID]; ok {
		// Reusing a lease avoids IP churn when the client reconnects or tcpx
		// transparently re-establishes the encrypted TCP connection.
		lease.UpdatedAt = time.Now()
		pool.byUser[userID] = lease
		return lease, nil
	}
	ip, err := pool.allocateLocked()
	if err != nil {
		return Lease{}, err
	}
	lease := Lease{
		RoomID:       roomID,
		UserID:       userID,
		IP:           netutil.Uint32ToIP(ip).String(),
		SubnetMask:   pool.mask,
		Gateway:      netutil.Uint32ToIP(pool.gateway).String(),
		DNS:          append([]string(nil), pool.dns...),
		MTU:          pool.mtu,
		LeaseSeconds: int64(pool.leaseTTL.Seconds()),
		UpdatedAt:    time.Now(),
	}
	pool.byUser[userID] = lease
	pool.ownerByIP[ip] = userID
	return lease, nil
}

// ensurePoolLocked lazily builds a room address pool from the room CIDR stored
// in MySQL. The caller must hold Manager.mu.
func (m *Manager) ensurePoolLocked(roomID int64, networkCIDR string, dns []string, mtu int) (*roomPool, error) {
	if existing := m.rooms[roomID]; existing != nil {
		return existing, nil
	}
	ip, ipNet, err := net.ParseCIDR(networkCIDR)
	if err != nil {
		return nil, err
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("room network must be IPv4")
	}
	prefix, bits := ipNet.Mask.Size()
	if bits != 32 || prefix > 30 {
		return nil, fmt.Errorf("unsupported room prefix: %d", prefix)
	}
	if mtu <= 0 {
		mtu = 1300
	}
	network := netutil.IPToUint32(ip4)
	size := uint32(1) << uint(32-prefix)
	broadcast := network + size - 1
	gateway := broadcast - 1
	next := network + 1
	pool := &roomPool{
		roomID:      roomID,
		network:     network,
		broadcast:   broadcast,
		next:        next,
		gateway:     gateway,
		mask:        netutil.PrefixToMask(prefix),
		dns:         append([]string(nil), dns...),
		mtu:         mtu,
		byUser:      make(map[int64]Lease),
		ownerByIP:   make(map[uint32]int64),
		leaseTTL:    24 * time.Hour,
		networkCIDR: networkCIDR,
	}
	m.rooms[roomID] = pool
	return pool, nil
}

// allocateLocked walks the pool once and returns the first unused host address.
// The caller must hold Manager.mu.
func (p *roomPool) allocateLocked() (uint32, error) {
	start := p.next
	for {
		ip := p.next
		p.advance()
		if ip != p.network && ip != p.broadcast && ip != p.gateway {
			if _, exists := p.ownerByIP[ip]; !exists {
				return ip, nil
			}
		}
		if p.next == start {
			return 0, fmt.Errorf("room %d address pool exhausted", p.roomID)
		}
	}
}

func (p *roomPool) advance() {
	p.next++
	if p.next >= p.broadcast {
		p.next = p.network + 1
	}
}
