package main

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/prometheus/common/log"
)

type lazyConn struct {
	m           sync.Mutex
	conn        net.Conn
	addr        string
	dialTimeout time.Duration
	readTimeout time.Duration
}

func newLazyConn(addr string, dialTimeout time.Duration, readTimeout time.Duration) (io.ReadWriteCloser, error) {
	l := &lazyConn{
		dialTimeout: dialTimeout,
		readTimeout: readTimeout,
		addr:        addr,
	}
	if err := l.connect(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *lazyConn) connect() error {
	conn, err := net.DialTimeout("tcp", l.addr, l.dialTimeout)
	if err != nil {
		l.conn = nil
		return err
	}
	l.conn = conn
	return nil
}

func (l *lazyConn) withTimeout() net.Conn {
	if l.readTimeout > 0 {
		err := l.conn.SetReadDeadline(time.Now().Add(l.readTimeout))
		if err != nil {
			log.Warnf("unable to set timeout: %s", err)
		}
	}
	return l.conn
}

// Read implements the io.Reader interface and attempt
// to reconnect to beanstalk in case of io.EOF.
func (l *lazyConn) Read(p []byte) (n int, err error) {
	l.m.Lock()
	defer l.m.Unlock()

	if l.conn == nil {
		if err := l.connect(); err != nil {
			return 0, io.ErrUnexpectedEOF
		}
	}

	n, err = l.withTimeout().Read(p)
	switch {
	case err == io.EOF:
		fallthrough
	case err == io.ErrUnexpectedEOF:
		l.conn = nil
	}
	return n, err
}

// Write implements the io.Writer interface and attempt
// to reconnect to beanstalk in case of io.EOF.
func (l *lazyConn) Write(p []byte) (n int, err error) {
	l.m.Lock()
	defer l.m.Unlock()

	if l.conn == nil {
		if err := l.connect(); err != nil {
			return 0, io.ErrClosedPipe
		}
	}

	n, err = l.withTimeout().Write(p)
	if n == 0 && err != io.ErrClosedPipe {
		l.conn = nil
	}
	return n, err
}

// Close the TCP connection.
func (l *lazyConn) Close() error {
	return l.conn.Close()
}
