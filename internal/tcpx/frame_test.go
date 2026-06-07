package tcpx

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestWriteFrameHandlesShortWrites(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	cfg := BaseConfig{}
	cfg.setDefaults()
	payload := []byte("hello over a deliberately short writer")
	done := make(chan error, 1)
	go func() {
		done <- writeFrame(shortWriteConn{Conn: client, max: 3}, cfg, FrameEncrypted, payload)
	}()

	frame, err := readFrame(server, cfg)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if frame.Type != FrameEncrypted {
		t.Fatalf("frame type=%d, want %d", frame.Type, FrameEncrypted)
	}
	if !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("payload mismatch")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write frame: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for writer")
	}
}

func TestWriteFullRejectsZeroLengthWrite(t *testing.T) {
	err := writeFull(zeroWriteConn{}, []byte("payload"))
	if err == nil {
		t.Fatalf("writeFull succeeded after zero-length write")
	}
}

type shortWriteConn struct {
	net.Conn
	max int
}

func (c shortWriteConn) Write(p []byte) (int, error) {
	if c.max > 0 && len(p) > c.max {
		p = p[:c.max]
	}
	return c.Conn.Write(p)
}

type zeroWriteConn struct{}

func (zeroWriteConn) Read([]byte) (int, error)         { return 0, nil }
func (zeroWriteConn) Write([]byte) (int, error)        { return 0, nil }
func (zeroWriteConn) Close() error                     { return nil }
func (zeroWriteConn) LocalAddr() net.Addr              { return nil }
func (zeroWriteConn) RemoteAddr() net.Addr             { return nil }
func (zeroWriteConn) SetDeadline(time.Time) error      { return nil }
func (zeroWriteConn) SetReadDeadline(time.Time) error  { return nil }
func (zeroWriteConn) SetWriteDeadline(time.Time) error { return nil }
