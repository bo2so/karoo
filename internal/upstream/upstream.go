package upstream

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/carlosrabelo/karoo/internal/config"
	"github.com/carlosrabelo/karoo/internal/protocol"
)

const proxyUserAgent = "karoo/v0.0.1"

type Upstream struct {
	cfg *config.Config

	mu   sync.Mutex
	conn net.Conn
	br   *bufio.Reader
	bw   *bufio.Writer

	ex1     string
	ex2Size int

	reqID atomic.Int64

	respMu  sync.Mutex
	pending map[int64]PendingReq
}

type PendingReq struct {
	Cl     interface{}
	Method string
	Sent   time.Time
	OrigID *int64
}

func New(cfg *config.Config) *Upstream {
	return &Upstream{
		cfg:     cfg,
		pending: make(map[int64]PendingReq),
	}
}

func (u *Upstream) Dial(ctx context.Context) error {
	addr := net.JoinHostPort(u.cfg.Upstream.Host, strconv.Itoa(u.cfg.Upstream.Port))
	var c net.Conn
	var err error
	if u.cfg.Upstream.TLS {
		conf := &tls.Config{InsecureSkipVerify: u.cfg.Upstream.InsecureSkipVerify}
		c, err = tls.Dial("tcp", addr, conf)
	} else {
		c, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}
	if err != nil {
		return err
	}
	u.mu.Lock()
	u.conn = c
	u.br = bufio.NewReaderSize(c, u.cfg.Proxy.ReadBuf)
	u.bw = bufio.NewWriterSize(c, u.cfg.Proxy.WriteBuf)
	u.mu.Unlock()
	u.respMu.Lock()
	u.pending = make(map[int64]PendingReq)
	u.respMu.Unlock()
	return nil
}

func (u *Upstream) Close() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.conn != nil {
		_ = u.conn.Close()
		u.conn = nil
		u.br = nil
		u.bw = nil
	}
}

func (u *Upstream) SendRaw(line string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.conn == nil {
		return errors.New("upstream nil")
	}
	if _, err := u.bw.WriteString(line); err != nil {
		return err
	}
	return u.bw.Flush()
}

func (u *Upstream) Send(msg protocol.JSONMsg) (int64, error) {
	i := u.reqID.Add(1)
	msg.ID = &i
	b, _ := json.Marshal(msg)
	b = append(b, '\n')
	return i, u.SendRaw(string(b))
}

func (u *Upstream) SubscribeAuthorize() error {
	if _, err := u.Send(protocol.JSONMsg{Method: "mining.subscribe", Params: []interface{}{proxyUserAgent}}); err != nil {
		return err
	}
	_, err := u.Send(protocol.JSONMsg{Method: "mining.authorize", Params: []interface{}{u.cfg.Upstream.User, u.cfg.Upstream.Pass}})
	return err
}

func (u *Upstream) IsConnected() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.conn != nil
}

func (u *Upstream) GetReader() *bufio.Reader {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.br
}

func (u *Upstream) SetExtranonce(ex1 string, ex2Size int) {
	u.ex1 = ex1
	u.ex2Size = ex2Size
}

func (u *Upstream) GetExtranonce() (string, int) {
	return u.ex1, u.ex2Size
}

func (u *Upstream) AddPendingReq(id int64, req PendingReq) {
	u.respMu.Lock()
	defer u.respMu.Unlock()
	u.pending[id] = req
}

func (u *Upstream) GetPendingReq(id int64) (PendingReq, bool) {
	u.respMu.Lock()
	defer u.respMu.Unlock()
	req, ok := u.pending[id]
	if ok {
		delete(u.pending, id)
	}
	return req, ok
}
