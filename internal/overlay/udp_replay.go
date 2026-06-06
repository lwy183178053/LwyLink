package overlay

const defaultUDPReplayCacheSize = 4096

type UDPReplayCache struct {
	order []string
	seen  map[string]struct{}
	limit int
}

func NewUDPReplayCache(limit int) *UDPReplayCache {
	if limit <= 0 {
		limit = defaultUDPReplayCacheSize
	}
	return &UDPReplayCache{
		order: make([]string, 0, limit),
		seen:  make(map[string]struct{}, limit),
		limit: limit,
	}
}

func (c *UDPReplayCache) Accept(nonce [udpNonceLen]byte) bool {
	if c == nil {
		return true
	}
	key := string(nonce[:])
	if _, ok := c.seen[key]; ok {
		return false
	}
	c.seen[key] = struct{}{}
	c.order = append(c.order, key)
	if len(c.order) > c.limit {
		oldest := c.order[0]
		copy(c.order, c.order[1:])
		c.order = c.order[:len(c.order)-1]
		delete(c.seen, oldest)
	}
	return true
}
