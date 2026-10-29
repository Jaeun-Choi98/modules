package udpnet

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// Probe는 핑퐁으로 상대가 현재 수신 가능한 상태인지 확인함.
//
// 대상에서 온 패킷이라면 pong이 아니어도 모두 생존 신호로 봄.
// 마지막 수신 이후 Interval 이상 조용할 때만 ping을 보내므로, 트래픽이 있는 동안에는 ping을 생략함.
type Probe struct {
	// ping을 보낼 주소. 필수.
	Target string

	// ping 확인 주기. 필수.
	Interval time.Duration

	// 마지막 수신 후 이 시간이 지나면 수신 불가로 판단. 0이면 Interval*3.
	Timeout time.Duration

	// 전송할 ping 데이터. nil이면 ping 없이 수신 트래픽만 감시.
	Ping []byte

	// true를 반환한 패킷은 pong으로 보고 핸들러로 전달하지 않음. nil이면 모든 패킷을 핸들러로 전달.
	IsPong func(data []byte, src *net.UDPAddr) bool

	// 생존 신호로 볼 패킷인지 판단. nil이면 출발지 IP가 Target IP와 같은지로 판단 (포트는 무시).
	// 상대의 송신 포트가 수신 포트와 다를 수 있으므로 기본값은 IP만 비교함.
	// Target이 멀티캐스트·브로드캐스트 주소라면 반드시 지정해야 함.
	MatchSource func(src *net.UDPAddr) bool

	// 수신 가능 상태가 바뀔 때 호출됨. probe 고루틴에서 순서대로 호출됨.
	OnChange func(reachable bool)
}

type Prober struct {
	cfg     Probe
	target  *net.UDPAddr
	timeout time.Duration
	ep      *Endpoint

	mu        sync.Mutex
	lastSeen  time.Time
	reachable bool // OnChange 통지 기준 상태

	notify chan struct{}
}

func (e *Endpoint) StartProbe(cfg Probe) (*Prober, error) {
	if cfg.Interval <= 0 {
		return nil, errors.New("udpnet: probe interval must be positive")
	}

	if cfg.Target == "" {
		return nil, errors.New("udpnet: probe target is required")
	}
	target, err := net.ResolveUDPAddr("udp", cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("udpnet: resolve probe target: %w", err)
	}

	p := &Prober{
		cfg:     cfg,
		target:  target,
		timeout: cfg.Timeout,
		ep:      e,
		notify:  make(chan struct{}, 1),
	}
	if p.timeout <= 0 {
		p.timeout = cfg.Interval * 3
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx.Err() != nil {
		return nil, ErrEndpointClosed
	}
	if !e.started {
		return nil, ErrNotStarted
	}
	e.probes = append(e.probes, p)
	e.wg.Add(1)
	go p.run()
	return p, nil
}

// 대상이 현재 수신 가능한 상태인지. 마지막 수신 시각 기준으로 계산함.
func (p *Prober) Reachable() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isAlive()
}

// 대상에서 마지막으로 패킷을 받은 시각. 받은 적이 없으면 zero.
func (p *Prober) LastSeen() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSeen
}

func (p *Prober) isAlive() bool {
	return !p.lastSeen.IsZero() && time.Since(p.lastSeen) < p.timeout
}

func (p *Prober) matches(src *net.UDPAddr) bool {
	if p.cfg.MatchSource != nil {
		return p.cfg.MatchSource(src)
	}
	return src.IP.Equal(p.target.IP)
}

func (p *Prober) seen() {
	p.mu.Lock()
	p.lastSeen = time.Now()
	p.mu.Unlock()

	select {
	case p.notify <- struct{}{}:
	default:
	}
}

func (p *Prober) run() {
	defer p.ep.wg.Done()

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

	p.tick()
	for {
		select {
		case <-p.ep.ctx.Done():
			return
		case <-ticker.C:
			p.tick()
		case <-p.notify:
			p.evaluate()
		}
	}
}

func (p *Prober) tick() {
	p.evaluate()

	p.mu.Lock()
	idle := p.lastSeen.IsZero() || time.Since(p.lastSeen) >= p.cfg.Interval
	p.mu.Unlock()

	if idle && len(p.cfg.Ping) > 0 {
		if err := p.ep.SendTo(p.cfg.Ping, p.target); err != nil && p.ep.ctx.Err() == nil {
			log.Printf("[udpnet] probe ping to %s failed: %v", p.target, err)
		}
	}
}

func (p *Prober) evaluate() {
	p.mu.Lock()
	alive := p.isAlive()
	changed := alive != p.reachable
	p.reachable = alive
	p.mu.Unlock()

	if changed && p.cfg.OnChange != nil {
		safeCall(func() { p.cfg.OnChange(alive) })
	}
}

// 수신 패킷을 각 Probe에 반영함. pong으로 소비되었으면 true.
func (e *Endpoint) observe(data []byte, src *net.UDPAddr) (consumed bool) {
	e.mu.RLock()
	probes := e.probes
	e.mu.RUnlock()

	for _, p := range probes {
		safeCall(func() {
			if !p.matches(src) {
				return
			}
			p.seen()
			if p.cfg.IsPong != nil && p.cfg.IsPong(data, src) {
				consumed = true
			}
		})
	}
	return consumed
}
