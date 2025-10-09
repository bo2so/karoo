package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/carlosrabelo/karoo/internal/protocol"
)

type Client struct {
	c    net.Conn
	br   *bufio.Reader
	bw   *bufio.Writer
	addr string

	last atomic.Int64
	ok   atomic.Uint64
	bad  atomic.Uint64

	handshakeDone atomic.Bool

	worker           string
	upUser           string
	lastAccept       atomic.Int64
	extraNoncePrefix string
	extraNonceTrim   int

	diff atomic.Int64

	writeMu sync.Mutex
}

func New(conn net.Conn, readBuf, writeBuf int, upUser string) *Client {
	return &Client{
		c:      conn,
		br:     bufio.NewReaderSize(conn, readBuf),
		bw:     bufio.NewWriterSize(conn, writeBuf),
		addr:   conn.RemoteAddr().String(),
		upUser: upUser,
	}
}

func (cl *Client) WriteRaw(b []byte) error {
	cl.writeMu.Lock()
	defer cl.writeMu.Unlock()
	if cl.bw == nil {
		return errors.New("client writer nil")
	}
	if _, err := cl.bw.Write(b); err != nil {
		return err
	}
	return cl.bw.Flush()
}

func (cl *Client) WriteJSON(obj protocol.JSONMsg) error {
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return cl.WriteRaw(b)
}

func (cl *Client) WriteLine(line string) error {
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	return cl.WriteRaw([]byte(line))
}

func (cl *Client) GetAddr() string {
	return cl.addr
}

func (cl *Client) GetWorker() string {
	return cl.worker
}

func (cl *Client) SetWorker(worker string) {
	cl.worker = worker
}

func (cl *Client) GetUpUser() string {
	return cl.upUser
}

func (cl *Client) SetUpUser(upUser string) {
	cl.upUser = upUser
}

func (cl *Client) GetReader() *bufio.Reader {
	return cl.br
}

func (cl *Client) Close() error {
	return cl.c.Close()
}

func (cl *Client) SetReadDeadline(deadline time.Time) error {
	return cl.c.SetReadDeadline(deadline)
}

func (cl *Client) UpdateLast(timestamp int64) {
	cl.last.Store(timestamp)
}

func (cl *Client) SetHandshakeDone(done bool) {
	cl.handshakeDone.Store(done)
}

func (cl *Client) IsHandshakeDone() bool {
	return cl.handshakeDone.Load()
}

func (cl *Client) IncrementOK() {
	cl.ok.Add(1)
}

func (cl *Client) IncrementBad() {
	cl.bad.Add(1)
}

func (cl *Client) GetStats() (uint64, uint64) {
	return cl.ok.Load(), cl.bad.Load()
}

func (cl *Client) SetLastAccept(timestamp int64) int64 {
	return cl.lastAccept.Swap(timestamp)
}

func (cl *Client) SetExtraNonce(prefix string, trim int) {
	cl.extraNoncePrefix = prefix
	cl.extraNonceTrim = trim
}

func (cl *Client) GetExtraNonce() (string, int) {
	return cl.extraNoncePrefix, cl.extraNonceTrim
}

func (cl *Client) SetDiff(diff int64) {
	cl.diff.Store(diff)
}

func (cl *Client) GetDiff() int64 {
	return cl.diff.Load()
}
