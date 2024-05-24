// Code copied from https://github.com/libp2p/go-libp2p/blob/9bd85029550a084fca63ec6ff9184122cdf06591/p2p/muxer/mplex/conn.go
package p2p

import (
	"context"
	"net"
	"time"

	"github.com/libp2p/go-libp2p/core/network"

	mp "github.com/libp2p/go-mplex"
)

type conn mp.Multiplex

var _ network.MuxedConn = &conn{}

// NewMuxedConn constructs a new Conn from a *mp.Multiplex.
func NewMuxedConn(m *mp.Multiplex) network.MuxedConn {
	return (*conn)(m)
}

func (c *conn) Close() error {
	return c.mplex().Close()
}

func (c *conn) IsClosed() bool {
	return c.mplex().IsClosed()
}

// OpenStream creates a new stream.
func (c *conn) OpenStream(ctx context.Context) (network.MuxedStream, error) {
	s, err := c.mplex().NewStream(ctx)
	if err != nil {
		return nil, err
	}
	return (*stream)(s), nil
}

// AcceptStream accepts a stream opened by the other side.
func (c *conn) AcceptStream() (network.MuxedStream, error) {
	s, err := c.mplex().Accept()
	if err != nil {
		return nil, err
	}
	return (*stream)(s), nil
}

func (c *conn) mplex() *mp.Multiplex {
	return (*mp.Multiplex)(c)
}

// DefaultTransport has default settings for Transport
var DefaultTransport = &Transport{}

const ID = "/mplex/6.7.0"

var _ network.Multiplexer = &Transport{}

// Transport implements mux.Multiplexer that constructs
// mplex-backed muxed connections.
type Transport struct{}

func (t *Transport) NewConn(nc net.Conn, isServer bool, scope network.PeerScope) (network.MuxedConn, error) {
	m, err := mp.NewMultiplex(nc, isServer, scope)
	if err != nil {
		return nil, err
	}
	return NewMuxedConn(m), nil
}

// stream implements network.MuxedStream over mplex.Stream.
type stream mp.Stream

var _ network.MuxedStream = &stream{}

func (s *stream) Read(b []byte) (n int, err error) {
	n, err = s.mplex().Read(b)
	if err == mp.ErrStreamReset {
		err = network.ErrReset
	}

	return n, err
}

func (s *stream) Write(b []byte) (n int, err error) {
	n, err = s.mplex().Write(b)
	if err == mp.ErrStreamReset {
		err = network.ErrReset
	}

	return n, err
}

func (s *stream) Close() error {
	return s.mplex().Close()
}

func (s *stream) CloseWrite() error {
	return s.mplex().CloseWrite()
}

func (s *stream) CloseRead() error {
	return s.mplex().CloseRead()
}

func (s *stream) Reset() error {
	return s.mplex().Reset()
}

func (s *stream) SetDeadline(t time.Time) error {
	return s.mplex().SetDeadline(t)
}

func (s *stream) SetReadDeadline(t time.Time) error {
	return s.mplex().SetReadDeadline(t)
}

func (s *stream) SetWriteDeadline(t time.Time) error {
	return s.mplex().SetWriteDeadline(t)
}

func (s *stream) mplex() *mp.Stream {
	return (*mp.Stream)(s)
}
