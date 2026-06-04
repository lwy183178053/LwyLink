package tun

import (
	"errors"
	"log"
	"sync"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

const packetQueueSize = 4096

var ErrClosed = errors.New("tun tunnel closed")

// Tunnel hides the Wintun device behind channels. The overlay code can treat
// it as packet IO, while Windows-specific adapter creation stays in tun_windows.go.
type Tunnel struct {
	dev wgtun.Device

	name string
	mtu  int

	batchSize int
	readBufs  [][]byte
	readSizes []int

	readChan  chan []byte
	writeChan chan []byte
	done      chan struct{}
	once      sync.Once
}

// NewTunnel creates the TUN device and starts read/write loops. Packets are
// copied before crossing channels so buffers owned by Wintun can be reused.
func NewTunnel(ifName string, mtu int) (*Tunnel, error) {
	if mtu <= 0 {
		mtu = 1300
	}
	dev, err := createTunDevice(ifName, mtu)
	if err != nil {
		return nil, err
	}
	realName, _ := dev.Name()
	batchSize := dev.BatchSize()
	if batchSize < 1 {
		batchSize = 1
	}
	t := &Tunnel{
		dev:       dev,
		name:      realName,
		mtu:       mtu,
		batchSize: batchSize,
		readBufs:  make([][]byte, batchSize),
		readSizes: make([]int, batchSize),
		readChan:  make(chan []byte, packetQueueSize),
		writeChan: make(chan []byte, packetQueueSize),
		done:      make(chan struct{}),
	}
	for i := range t.readBufs {
		t.readBufs[i] = make([]byte, mtu+128)
	}
	go t.readLoop()
	go t.writeLoop()
	return t, nil
}

func (t *Tunnel) Name() string {
	return t.name
}

func (t *Tunnel) ReadChan() <-chan []byte {
	return t.readChan
}

func (t *Tunnel) Write(pkt []byte) error {
	if len(pkt) == 0 {
		return nil
	}
	// Copy before enqueueing because callers may reuse their packet slice after
	// Write returns.
	cp := make([]byte, len(pkt))
	copy(cp, pkt)
	select {
	case <-t.done:
		return ErrClosed
	case t.writeChan <- cp:
		return nil
	}
}

func (t *Tunnel) Close() error {
	var err error
	t.once.Do(func() {
		close(t.done)
		err = t.dev.Close()
	})
	return err
}

func (t *Tunnel) readLoop() {
	defer close(t.readChan)
	for {
		select {
		case <-t.done:
			return
		default:
		}
		n, err := t.dev.Read(t.readBufs, t.readSizes, 0)
		if err != nil {
			_ = t.Close()
			return
		}
		for i := 0; i < n; i++ {
			size := t.readSizes[i]
			if size <= 0 || size > len(t.readBufs[i]) {
				continue
			}
			pkt := make([]byte, size)
			copy(pkt, t.readBufs[i][:size])
			select {
			case <-t.done:
				return
			case t.readChan <- pkt:
			}
		}
	}
}

func (t *Tunnel) writeLoop() {
	batch := make([][]byte, 0, t.batchSize)
	for {
		batch = batch[:0]
		select {
		case <-t.done:
			return
		case pkt := <-t.writeChan:
			batch = append(batch, pkt)
		}
	drain:
		for len(batch) < t.batchSize {
			select {
			case <-t.done:
				return
			case pkt := <-t.writeChan:
				batch = append(batch, pkt)
			default:
				break drain
			}
		}
		if _, err := t.dev.Write(batch, 0); err != nil {
			log.Printf("tun write failed: %v", err)
			_ = t.Close()
			return
		}
	}
}
