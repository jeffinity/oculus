package eks

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/pkg/errors"

	"github.com/jeffinity/oculus/app/eks-server/internal/conf"
)

const (
	defaultMinSize             = 0
	defaultMaxSize             = 1
	defaultCommandTimeout      = 30 * time.Second
	defaultConnectTimeout      = 45 * time.Second
	defaultIdleTTL             = 20 * time.Minute
	defaultHealthCheckInterval = 60 * time.Second
	defaultHealthCheckTimeout  = 10 * time.Second
	defaultHealthCheckCommand  = "echo oculus-eks-health"
)

var newPoolSessionFactory = func(p *Pool) func(context.Context) (*Session, error) {
	return p.newSession
}

type Pool struct {
	cluster             string
	env                 string
	kind                PoolKind
	jumpserver          *conf.JumpServer
	envConf             *conf.EKSEnv
	minSize             int
	maxSize             int
	commandTimeout      time.Duration
	idleTTL             time.Duration
	healthCheckInterval time.Duration
	healthCheckTimeout  time.Duration
	healthCheckCommand  string
	logger              *log.Helper
	sessionFactory      func(context.Context) (*Session, error)

	mu        sync.Mutex
	cond      *sync.Cond
	idle      []*Session
	total     int
	active    int
	closed    bool
	closeOnce sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewPool(
	cluster string,
	env string,
	kind PoolKind,
	jumpserver *conf.JumpServer,
	envConf *conf.EKSEnv,
	c *conf.Pool,
	logger *log.Helper,
) (*Pool, error) {
	minSize := int(c.GetMinSize())
	if minSize < 0 {
		minSize = defaultMinSize
	}
	maxSize := int(c.GetMaxSize())
	if maxSize <= 0 {
		maxSize = defaultMaxSize
	}
	if minSize > maxSize {
		return nil, fmt.Errorf("%s pool min_size cannot exceed max_size", kind)
	}

	poolCtx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		cluster:             cluster,
		env:                 env,
		kind:                kind,
		jumpserver:          jumpserver,
		envConf:             envConf,
		minSize:             minSize,
		maxSize:             maxSize,
		commandTimeout:      durationOr(c.GetCommandTimeout().AsDuration(), defaultCommandTimeout),
		idleTTL:             durationOr(c.GetIdleTtl().AsDuration(), defaultIdleTTL),
		healthCheckInterval: durationOr(c.GetHealthCheckInterval().AsDuration(), defaultHealthCheckInterval),
		healthCheckTimeout:  durationOr(c.GetHealthCheckTimeout().AsDuration(), defaultHealthCheckTimeout),
		healthCheckCommand:  strings.TrimSpace(c.GetHealthCheckCommand()),
		logger:              logger,
		cancel:              cancel,
		done:                make(chan struct{}),
	}
	p.cond = sync.NewCond(&p.mu)
	p.sessionFactory = newPoolSessionFactory(p)
	if p.healthCheckCommand == "" {
		p.healthCheckCommand = defaultHealthCheckCommand
	}

	go p.maintain(poolCtx)
	return p, nil
}

func (p *Pool) Execute(ctx context.Context, req CommandRequest) (*CommandResult, error) {
	if err := validateSingleLineCommand(req.Command); err != nil {
		return nil, err
	}

	timeout := durationOr(req.Timeout, p.commandTimeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	session, err := p.acquire(ctx)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	result, execErr := session.Run(ctx, req.Command, timeout)
	if result == nil {
		result = &CommandResult{ExitCode: -1}
		if execErr == nil {
			execErr = errors.New("command returned empty result")
		}
	}
	result.Cluster = p.cluster
	result.Env = p.env
	result.Pool = string(p.kind)
	result.Command = req.Command
	result.DurationMS = time.Since(start).Milliseconds()

	if execErr != nil || result.ExitCode != 0 {
		p.release(session, false)
		return result, execErr
	}
	p.release(session, true)
	return result, nil
}

func (p *Pool) Snapshot() PoolSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return PoolSnapshot{
		MinSize: p.minSize,
		MaxSize: p.maxSize,
		Idle:    len(p.idle),
		Active:  p.active,
		Total:   p.total,
	}
}

func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		idle := p.idle
		p.idle = nil
		p.cond.Broadcast()
		p.mu.Unlock()

		p.cancel()
		for _, session := range idle {
			_ = session.Close()
		}
		<-p.done
	})
}

func (p *Pool) acquire(ctx context.Context) (*Session, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, errors.New("pool is closed")
		}
		if session := p.popIdleLocked(); session != nil {
			p.active++
			p.mu.Unlock()
			return session, nil
		}
		if p.total < p.maxSize {
			p.total++
			p.active++
			p.mu.Unlock()

			session, err := p.sessionFactory(ctx)
			if err != nil {
				p.decrementCounts()
				return nil, err
			}
			return session, nil
		}
		p.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *Pool) release(session *Session, reusable bool) {
	if session == nil {
		p.finishActive(nil)
		return
	}
	if !reusable || !session.IsAlive() || session.IsExpired(p.idleTTL) {
		_ = session.Close()
		p.finishActive(nil)
		return
	}
	p.finishActive(session)
}

func (p *Pool) finishActive(session *Session) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active > 0 {
		p.active--
	}
	if session == nil {
		if p.total > 0 {
			p.total--
		}
	} else if p.closed {
		_ = session.Close()
		if p.total > 0 {
			p.total--
		}
	} else {
		p.idle = append(p.idle, session)
	}
	p.cond.Broadcast()
}

func (p *Pool) decrementCounts() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active > 0 {
		p.active--
	}
	if p.total > 0 {
		p.total--
	}
	p.cond.Broadcast()
}

func (p *Pool) popIdleLocked() *Session {
	for len(p.idle) > 0 {
		last := len(p.idle) - 1
		session := p.idle[last]
		p.idle = p.idle[:last]
		if session != nil && session.IsAlive() && !session.IsExpired(p.idleTTL) {
			return session
		}
		if session != nil {
			_ = session.Close()
		}
		if p.total > 0 {
			p.total--
		}
	}
	return nil
}

func (p *Pool) maintain(ctx context.Context) {
	defer close(p.done)
	p.ensureMinSize(ctx)

	ticker := time.NewTicker(p.healthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.reapIdle()
			p.checkIdle(ctx)
			p.ensureMinSize(ctx)
		}
	}
}

func (p *Pool) ensureMinSize(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		p.mu.Lock()
		if p.closed || p.total >= p.minSize || p.total >= p.maxSize {
			p.mu.Unlock()
			return
		}
		p.total++
		p.mu.Unlock()

		session, err := p.sessionFactory(ctx)
		if err != nil {
			p.mu.Lock()
			if p.total > 0 {
				p.total--
			}
			p.cond.Broadcast()
			p.mu.Unlock()
			if p.logger != nil {
				p.logger.Warnf("warm eks pool session failed cluster=%s env=%s pool=%s: %+v", p.cluster, p.env, p.kind, err)
			}
			return
		}

		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			_ = session.Close()
			return
		}
		p.idle = append(p.idle, session)
		p.cond.Broadcast()
		p.mu.Unlock()
	}
}

func (p *Pool) reapIdle() {
	var closing []*Session

	p.mu.Lock()
	kept := p.idle[:0]
	for _, session := range p.idle {
		if session == nil || !session.IsAlive() {
			if p.total > 0 {
				p.total--
			}
			if session != nil {
				closing = append(closing, session)
			}
			continue
		}
		if session.IsExpired(p.idleTTL) && p.total > p.minSize {
			if p.total > 0 {
				p.total--
			}
			closing = append(closing, session)
			continue
		}
		kept = append(kept, session)
	}
	p.idle = kept
	p.cond.Broadcast()
	p.mu.Unlock()

	for _, session := range closing {
		_ = session.Close()
	}
}

func (p *Pool) checkIdle(ctx context.Context) {
	for {
		session := p.takeIdleForCheck()
		if session == nil {
			return
		}

		checkCtx, cancel := context.WithTimeout(ctx, p.healthCheckTimeout)
		result, err := session.Run(checkCtx, p.healthCheckCommand, p.healthCheckTimeout)
		cancel()
		healthy := err == nil && result != nil && result.ExitCode == 0 && strings.Contains(result.Output, "oculus-eks-health")
		if !healthy && p.logger != nil {
			p.logger.Warnf("eks pool health check failed cluster=%s env=%s pool=%s err=%+v", p.cluster, p.env, p.kind, err)
		}
		p.release(session, healthy)
	}
}

func (p *Pool) takeIdleForCheck() *Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || len(p.idle) == 0 {
		return nil
	}
	last := len(p.idle) - 1
	session := p.idle[last]
	p.idle = p.idle[:last]
	p.active++
	return session
}

func (p *Pool) newSession(ctx context.Context) (*Session, error) {
	connectTimeout := durationOr(p.jumpserver.GetConnectTimeout().AsDuration(), defaultConnectTimeout)
	sessionCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	opts := SessionOptions{
		Cluster:        p.cluster,
		Env:            p.env,
		Pool:           p.kind,
		Host:           p.jumpserver.GetHost(),
		Port:           p.jumpserver.GetPort(),
		User:           p.jumpserver.GetUser(),
		IdentityFile:   p.jumpserver.GetIdentityFile(),
		PassphraseEnv:  p.jumpserver.GetIdentityPassphraseEnv(),
		SOCKS5Proxy:    p.jumpserver.GetSocks5Proxy(),
		IgnoreHostKey:  p.jumpserver.GetInsecureIgnoreHostKey(),
		Asset:          p.envConf.GetAsset(),
		OptPrompt:      p.envConf.GetOptPrompt(),
		ShellPrompt:    p.envConf.GetShellPrompt(),
		ConnectTimeout: connectTimeout,
		Logger:         p.logger,
	}
	return NewSession(sessionCtx, opts)
}

func validateSingleLineCommand(command string) error {
	clean := strings.TrimSpace(command)
	if clean == "" {
		return errors.New("command is required")
	}
	if strings.Contains(clean, "\x00") || strings.ContainsAny(clean, "\r\n") {
		return errors.New("command must be a single line")
	}
	return nil
}
