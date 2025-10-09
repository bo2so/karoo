package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/carlosrabelo/karoo/internal/client"
	"github.com/carlosrabelo/karoo/internal/config"
	"github.com/carlosrabelo/karoo/internal/metrics"
	"github.com/carlosrabelo/karoo/internal/protocol"
	"github.com/carlosrabelo/karoo/internal/upstream"
	"github.com/carlosrabelo/karoo/internal/utils"
)

const extraNoncePrefixBytes = 1

type Proxy struct {
	cfg *config.Config
	up  *upstream.Upstream
	mx  *metrics.Metrics

	clMu          sync.RWMutex
	clients       map[*client.Client]struct{}
	prefixCounter atomic.Uint64

	upReady atomic.Bool
	readyMu sync.Mutex
	readyCh chan struct{}

	subMu       sync.Mutex
	pendingSubs map[*client.Client]*int64
}

func New(cfg *config.Config) *Proxy {
	return &Proxy{
		cfg:         cfg,
		up:          upstream.New(cfg),
		mx:          metrics.New(),
		clients:     make(map[*client.Client]struct{}),
		readyCh:     make(chan struct{}),
		pendingSubs: make(map[*client.Client]*int64),
	}
}

func (p *Proxy) upstreamReady() bool {
	ex1, ex2Size := p.up.GetExtranonce()
	return p.upReady.Load() && ex2Size > 0 && ex1 != ""
}

func (p *Proxy) enqueuePendingSubscribe(cl *client.Client, id *int64) {
	if p.upstreamReady() {
		p.respondSubscribe(cl, utils.CopyID(id))
		return
	}
	copy := utils.CopyID(id)
	p.subMu.Lock()
	defer p.subMu.Unlock()
	if p.pendingSubs == nil {
		p.pendingSubs = make(map[*client.Client]*int64)
	}
	p.pendingSubs[cl] = copy
}

func (p *Proxy) removePendingSubscribe(cl *client.Client) {
	p.subMu.Lock()
	defer p.subMu.Unlock()
	delete(p.pendingSubs, cl)
}

func (p *Proxy) flushPendingSubscribes() {
	p.subMu.Lock()
	if len(p.pendingSubs) == 0 {
		p.subMu.Unlock()
		return
	}
	pending := make(map[*client.Client]*int64, len(p.pendingSubs))
	for cl, id := range p.pendingSubs {
		pending[cl] = id
	}
	p.pendingSubs = make(map[*client.Client]*int64)
	p.subMu.Unlock()

	for cl, id := range pending {
		p.respondSubscribe(cl, id)
	}
}

func (p *Proxy) respondSubscribe(cl *client.Client, id *int64) {
	if !p.upstreamReady() {
		p.enqueuePendingSubscribe(cl, id)
		return
	}
	p.assignNoncePrefix(cl)
	ex1Resp, ex2Resp := p.up.GetExtranonce()
	prefix, trim := cl.GetExtraNonce()
	if prefix != "" && trim > 0 {
		if ex2Resp > trim {
			ex1Resp = ex1Resp + prefix
			ex2Resp = ex2Resp - trim
		} else {
			cl.SetExtraNonce("", 0)
		}
	}
	resp := protocol.JSONMsg{ID: id, Result: []interface{}{[]interface{}{}, ex1Resp, ex2Resp}}
	_ = cl.WriteJSON(resp)
}

func (p *Proxy) assignNoncePrefix(cl *client.Client) {
	prefix, _ := cl.GetExtraNonce()
	if prefix != "" {
		return
	}
	if extraNoncePrefixBytes <= 0 {
		return
	}
	_, ex2Size := p.up.GetExtranonce()
	if ex2Size <= extraNoncePrefixBytes {
		return
	}
	bits := extraNoncePrefixBytes * 8
	if bits <= 0 || bits >= 64 {
		return
	}
	mask := (uint64(1) << bits) - 1
	if mask == 0 {
		return
	}
	val := p.prefixCounter.Add(1) & mask
	prefixStr := fmt.Sprintf("%0*X", extraNoncePrefixBytes*2, val)
	cl.SetExtraNonce(prefixStr, extraNoncePrefixBytes)
}

func (p *Proxy) AcceptLoop(ctx context.Context) error {
	ln, err := net.Listen("tcp", p.cfg.Proxy.Listen)
	if err != nil {
		return err
	}
	log.Printf("proxy: listening on %s", p.cfg.Proxy.Listen)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("accept err: %v", err)
			continue
		}
		if p.mx.ClientsActive.Load() >= int64(p.cfg.Proxy.MaxClients) {
			log.Printf("rejecting client: max reached")
			_ = conn.Close()
			continue
		}
		cli := client.New(conn, p.cfg.Proxy.ReadBuf, p.cfg.Proxy.WriteBuf, p.cfg.Upstream.User)
		cli.UpdateLast(time.Now().UnixMilli())
		cli.SetDiff(int64(p.cfg.VarDiff.MinDiff))

		p.clMu.Lock()
		p.clients[cli] = struct{}{}
		p.clMu.Unlock()
		p.mx.ClientsActive.Add(1)
		log.Printf("client connected: %s", cli.GetAddr())

		go p.clientLoop(ctx, cli)
	}
}

func (p *Proxy) clientLoop(ctx context.Context, cl *client.Client) {
	defer func() {
		p.removePendingSubscribe(cl)
		p.clMu.Lock()
		delete(p.clients, cl)
		p.clMu.Unlock()
		p.mx.ClientsActive.Add(-1)
		_ = cl.Close()
		log.Printf("client closed: %s", cl.GetAddr())
	}()

	sc := bufio.NewScanner(cl.GetReader())
	buf := make([]byte, 0, p.cfg.Proxy.ReadBuf)
	sc.Buffer(buf, 1024*1024)

	idle := p.cfg.Proxy.ClientIdleMs
	for {
		if idle > 0 && !cl.IsHandshakeDone() {
			_ = cl.SetReadDeadline(time.Now().Add(time.Duration(idle) * time.Millisecond))
		} else {
			_ = cl.SetReadDeadline(time.Time{})
		}
		if !sc.Scan() {
			if err := sc.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
				log.Printf("client scan err %s: %v", cl.GetAddr(), err)
			}
			return
		}
		line := sc.Text()
		cl.UpdateLast(time.Now().UnixMilli())

		var msg protocol.JSONMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		switch msg.Method {
		case "mining.subscribe":
			if p.upstreamReady() {
				p.respondSubscribe(cl, utils.CopyID(msg.ID))
				break
			}
			p.enqueuePendingSubscribe(cl, msg.ID)
			continue

		case "mining.authorize":
			if arr, ok := msg.Params.([]interface{}); ok && len(arr) > 0 {
				if s, ok := arr[0].(string); ok {
					cl.SetWorker(s)
				}
			}
			p.forwardToUpstream(cl, msg.Method, msg.Params, msg.ID)
			break

		case "mining.submit":
			if arr, ok := msg.Params.([]interface{}); ok && len(arr) > 0 {
				upUser := cl.GetUpUser()
				if upUser == "" {
					upUser = p.cfg.Upstream.User
					cl.SetUpUser(upUser)
				}
				arr[0] = upUser
				prefix, trim := cl.GetExtraNonce()
				_, ex2Size := p.up.GetExtranonce()
				if len(arr) > 2 && prefix != "" && trim > 0 {
					if s, ok := arr[2].(string); ok {
						sUp := strings.ToUpper(s)
						expectedLen := (ex2Size - trim) * 2
						switch {
						case len(sUp) == expectedLen:
							sUp = prefix + sUp
						case len(sUp) == ex2Size*2:
							if !strings.HasPrefix(sUp, prefix) {
								sUp = prefix + sUp[len(prefix):]
							}
						default:
							if !strings.HasPrefix(sUp, prefix) {
								sUp = prefix + sUp
							}
						}
						arr[2] = sUp
					}
				}
				msg.Params = arr
			}
			p.forwardToUpstream(cl, "mining.submit", msg.Params, msg.ID)
			break
		default:
			if strings.HasPrefix(msg.Method, "mining.") {
				p.forwardToUpstream(cl, msg.Method, msg.Params, msg.ID)
				break
			}
		}
	}
}

func (p *Proxy) forwardToUpstream(cl *client.Client, method string, params interface{}, id *int64) bool {
	if !p.up.IsConnected() {
		_ = cl.WriteJSON(protocol.JSONMsg{ID: id, Result: false, Error: []interface{}{-1, "Upstream down", nil}})
		return false
	}
	origID := utils.CopyID(id)
	upID, err := p.up.Send(protocol.JSONMsg{Method: method, Params: params})
	if err != nil {
		_ = cl.WriteJSON(protocol.JSONMsg{ID: id, Result: false, Error: []interface{}{-1, "Forward error", nil}})
		return false
	}
	p.up.AddPendingReq(upID, upstream.PendingReq{Cl: cl, Method: method, Sent: time.Now(), OrigID: origID})
	return true
}

func (p *Proxy) UpstreamLoop(ctx context.Context) {
	min := time.Duration(p.cfg.Upstream.BackoffMinMs) * time.Millisecond
	max := time.Duration(p.cfg.Upstream.BackoffMaxMs) * time.Millisecond

	for ctx.Err() == nil {
		if err := p.up.Dial(ctx); err != nil {
			d := utils.Backoff(min, max)
			log.Printf("upstream dial fail: %v; retry in %s", err, d)
			time.Sleep(d)
			continue
		}
		p.mx.UpConnected.Store(true)
		log.Printf("upstream connected")

		if err := p.up.SubscribeAuthorize(); err != nil {
			log.Printf("handshake err: %v", err)
			p.up.Close()
			p.mx.UpConnected.Store(false)
			continue
		}

		sc := bufio.NewScanner(p.up.GetReader())
		buf := make([]byte, 0, p.cfg.Proxy.ReadBuf)
		sc.Buffer(buf, 1024*1024)

		for sc.Scan() {
			line := sc.Text()

			var msg protocol.JSONMsg
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}

			if msg.Method != "" {
				switch msg.Method {
				case "mining.set_difficulty":
					if arr, ok := msg.Params.([]interface{}); ok && len(arr) > 0 {
						if v, ok := arr[0].(float64); ok {
							p.mx.LastSetDiff.Store(int64(v))
						}
					}
					p.broadcast(line)

				case "mining.notify":
					p.mx.LastNotifyUnix.Store(time.Now().Unix())
					if arr, ok := msg.Params.([]interface{}); ok {
						var jobID, nbits string
						var clean bool
						if len(arr) > 0 {
							if s, ok := arr[0].(string); ok {
								jobID = s
							}
						}
						if len(arr) > 6 {
							if s, ok := arr[6].(string); ok {
								nbits = s
							}
						}
						if len(arr) > 8 {
							switch v := arr[8].(type) {
							case bool:
								clean = v
							case string:
								clean = strings.EqualFold(v, "true")
							}
						}
						if clean {
							diff := utils.DiffFromBits(nbits)
							log.Printf("new job job=%s diff=%.6g", jobID, diff)
						}
					}
					p.broadcast(line)

				default:
					if !p.cfg.Compat.StrictBroadcast && strings.HasPrefix(msg.Method, "mining.") {
						p.broadcast(line)
					}
				}
				continue
			}

			if msg.Result != nil && msg.ID != nil {
				if *msg.ID == 1 {
					log.Printf("subscribe result: %v", msg.Result)
					ex1, ex2, ok := utils.ParseExtranonceResult(msg.Result)
					if ok {
						p.up.SetExtranonce(ex1, ex2)
						p.readyMu.Lock()
						if !p.upReady.Load() {
							p.upReady.Store(true)
							close(p.readyCh)
						}
						p.readyMu.Unlock()
						p.flushPendingSubscribes()
					} else if !p.upReady.Load() {
						log.Printf("subscribe result missing extranonce fields: %v", msg.Result)
					}
				}

				req, ok := p.up.GetPendingReq(*msg.ID)
				if ok && req.Cl != nil {
					cl := req.Cl.(*client.Client)
					if req.OrigID != nil {
						msg.ID = req.OrigID
					} else {
						msg.ID = nil
					}
					_ = cl.WriteJSON(msg)
					if req.Method == "mining.submit" {
						p.handleSubmitResponse(cl, msg, req)
					}
					if req.Method == "mining.authorize" {
						if res, ok := msg.Result.(bool); ok && res {
							cl.SetHandshakeDone(true)
						}
					}
				}
			}
		}

		if err := sc.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Printf("upstream read err: %v", err)
		}
		p.up.Close()
		p.mx.UpConnected.Store(false)

		p.readyMu.Lock()
		p.upReady.Store(false)
		p.readyCh = make(chan struct{})
		p.readyMu.Unlock()

		d := utils.Backoff(min, max)
		log.Printf("upstream disconnected; retry in %s", d)
		time.Sleep(d)
	}
}

func (p *Proxy) handleSubmitResponse(cl *client.Client, msg protocol.JSONMsg, req upstream.PendingReq) {
	success := false
	if b, ok := msg.Result.(bool); ok {
		success = b
	}
	if success {
		p.mx.SharesOK.Add(1)
		cl.IncrementOK()
	} else {
		p.mx.SharesBad.Add(1)
		cl.IncrementBad()
	}
	latency := time.Since(req.Sent)
	var sincePrev time.Duration
	if success {
		nowMs := time.Now().UnixMilli()
		prev := cl.SetLastAccept(nowMs)
		if prev > 0 {
			sincePrev = time.Duration(nowMs-prev) * time.Millisecond
		}
	}
	totalOK, totalBad := cl.GetStats()
	totalShares := totalOK + totalBad
	status := "Rejected"
	if success {
		status = "Accepted"
	}
	worker := cl.GetWorker()
	if worker == "" {
		worker = cl.GetAddr()
	}
	log.Printf("share %s worker=%s share=%d ok=%d bad=%d since_prev=%s latency=%s", status, worker, totalShares, totalOK, totalBad, utils.FmtDuration(sincePrev), latency)
}

func (p *Proxy) broadcast(line string) {
	p.clMu.RLock()
	defer p.clMu.RUnlock()
	for cl := range p.clients {
		_ = cl.WriteLine(line)
	}
}

func (p *Proxy) VardiffLoop(ctx context.Context) {
	if !p.cfg.VarDiff.Enabled {
		return
	}
	t := time.NewTicker(time.Duration(p.cfg.VarDiff.AdjustEveryMs) * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.adjustDiffOnce()
		}
	}
}

func (p *Proxy) adjustDiffOnce() {
	p.clMu.RLock()
	defer p.clMu.RUnlock()
	for cl := range p.clients {
		d := cl.GetDiff()
		if d <= 0 {
			d = int64(p.cfg.VarDiff.MinDiff)
		}
		msg := protocol.JSONMsg{Method: "mining.set_difficulty", Params: []interface{}{float64(d)}}
		_ = cl.WriteJSON(msg)
	}
}

func (p *Proxy) HTTPServe(ctx context.Context) {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		type clientView struct {
			IP     string `json:"ip"`
			Worker string `json:"worker"`
			UpUser string `json:"upstream_user"`
			OK     uint64 `json:"ok"`
			Bad    uint64 `json:"bad"`
		}
		p.clMu.RLock()
		var clv []clientView
		for cl := range p.clients {
			ok, bad := cl.GetStats()
			clv = append(clv, clientView{
				IP:     cl.GetAddr(),
				Worker: cl.GetWorker(),
				UpUser: cl.GetUpUser(),
				OK:     ok,
				Bad:    bad,
			})
		}
		p.clMu.RUnlock()
		ex1, ex2Size := p.up.GetExtranonce()
		out := map[string]interface{}{
			"upstream":         p.mx.UpConnected.Load(),
			"extranonce1":      ex1,
			"extranonce2_size": ex2Size,
			"last_notify_unix": p.mx.LastNotifyUnix.Load(),
			"last_diff":        p.mx.LastSetDiff.Load(),
			"shares_ok":        p.mx.SharesOK.Load(),
			"shares_bad":       p.mx.SharesBad.Load(),
			"clients":          clv,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	srv := &http.Server{Addr: p.cfg.HTTP.Listen}
	go func() {
		<-ctx.Done()
		ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx2)
	}()
	log.Printf("http: listening on %s", p.cfg.HTTP.Listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("http err: %v", err)
	}
}

func (p *Proxy) ReportLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	start := time.Now()
	last := start
	lastOK := p.mx.SharesOK.Load()
	lastBad := p.mx.SharesBad.Load()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			totalOK := p.mx.SharesOK.Load()
			totalBad := p.mx.SharesBad.Load()
			deltaOK := totalOK - lastOK
			deltaBad := totalBad - lastBad
			submittedInterval := deltaOK + deltaBad
			submittedTotal := totalOK + totalBad
			intervalDur := now.Sub(last)
			totalDur := now.Sub(start)
			var rateInterval, rateTotal float64
			if minutes := intervalDur.Minutes(); minutes > 0 {
				rateInterval = float64(submittedInterval) / minutes
			}
			if minutes := totalDur.Minutes(); minutes > 0 {
				rateTotal = float64(submittedTotal) / minutes
			}
			var accInterval, accTotal float64
			if submittedInterval > 0 {
				accInterval = (float64(deltaOK) / float64(submittedInterval)) * 100
			}
			if submittedTotal > 0 {
				accTotal = (float64(totalOK) / float64(submittedTotal)) * 100
			}
			log.Printf("Periodic Report interval=%10s total=%10s | submitted %d/%d (acc %.1f%% / %.1f%%) | rejects %d/%d | rate %.2f/min (overall %.2f/min)", intervalDur.Round(time.Second), totalDur.Round(time.Second), deltaOK, totalOK, accInterval, accTotal, deltaBad, totalBad, rateInterval, rateTotal)
			last = now
			lastOK = totalOK
			lastBad = totalBad
		}
	}
}

func (p *Proxy) UpstreamManager(ctx context.Context, idleGrace time.Duration) {
	var upCancel context.CancelFunc
	var upCtx context.Context
	upstreamRunning := false

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if upstreamRunning && upCancel != nil {
				upCancel()
			}
			return
		case <-ticker.C:
			haveClients := p.mx.ClientsActive.Load() > 0
			if haveClients && !upstreamRunning {
				upCtx, upCancel = context.WithCancel(ctx)
				go p.UpstreamLoop(upCtx)
				upstreamRunning = true
				log.Printf("up-manager: clients present -> upstream started")
			}
			if upstreamRunning && !haveClients {
				deadline := time.Now().Add(idleGrace)
				for time.Now().Before(deadline) {
					if p.mx.ClientsActive.Load() > 0 || ctx.Err() != nil {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
				if p.mx.ClientsActive.Load() == 0 && ctx.Err() == nil {
					if upCancel != nil {
						upCancel()
					}
					upstreamRunning = false
					p.up.Close()
					p.mx.UpConnected.Store(false)

					p.readyMu.Lock()
					p.upReady.Store(false)
					p.readyCh = make(chan struct{})
					p.readyMu.Unlock()

					log.Printf("up-manager: no clients for %s -> upstream stopped", idleGrace)
				}
			}
		}
	}
}

func (p *Proxy) GetMetrics() *metrics.Metrics {
	return p.mx
}
