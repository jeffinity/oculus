package eks

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/jeffinity/oculus/app/eks-server/internal/conf"
)

func TestValidateSingleLineCommand(t *testing.T) {
	restore := stubSessionFactory(func(context.Context) (*Session, error) {
		return fakeSession(0, "ok"), nil
	})
	defer restore()

	pool, err := NewPool("jumpserver-sg", "dev", PoolQuery, &conf.JumpServer{
		Host:         "jumpserver-sg.gainetics.io",
		User:         "jeff",
		IdentityFile: "~/.ssh/jump_rsa",
	}, &conf.EKSEnv{
		Name:  "dev",
		Asset: "asset",
	}, &conf.Pool{
		MaxSize: 1,
	}, nil)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Close()

	if _, err := pool.Execute(context.Background(), CommandRequest{Command: "kubectl get pods -A"}); err != nil {
		t.Fatalf("Execute() command error = %v", err)
	}
	if _, err := pool.Execute(context.Background(), CommandRequest{Command: "kubectl delete pod a"}); err != nil {
		t.Fatalf("Execute() unrestricted command error = %v", err)
	}
	if _, err := pool.Execute(context.Background(), CommandRequest{Command: "kubectl get pods\nkubectl delete pod a"}); err == nil {
		t.Fatalf("Execute() multiline command error = nil")
	}
}

func TestNewPoolRejectsMinGreaterThanMax(t *testing.T) {
	_, err := NewPool("jumpserver-sg", "dev", PoolQuery, &conf.JumpServer{
		Host:         "jumpserver-sg.gainetics.io",
		User:         "jeff",
		IdentityFile: "~/.ssh/jump_rsa",
	}, &conf.EKSEnv{Name: "dev", Asset: "asset"}, &conf.Pool{
		MinSize: 2,
		MaxSize: 1,
	}, nil)
	if err == nil {
		t.Fatalf("NewPool() error = nil")
	}
}

func TestPoolDoesNotExceedMaxSize(t *testing.T) {
	var created int32
	release := make(chan struct{})
	restore := stubSessionFactory(func(context.Context) (*Session, error) {
		atomic.AddInt32(&created, 1)
		return fakeSessionWithBlock(release), nil
	})
	defer restore()

	pool, err := NewPool("jumpserver-sg", "dev", PoolQuery, testJumpServer(), testEnv(), &conf.Pool{
		MaxSize: 1,
	}, nil)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Close()

	done := make(chan struct{})
	go func() {
		_, _ = pool.Execute(context.Background(), CommandRequest{Command: "kubectl get pods"})
		close(done)
	}()
	waitUntil(t, func() bool { return pool.Snapshot().Active == 1 })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = pool.Execute(ctx, CommandRequest{Command: "kubectl get pods"})
	if err == nil {
		t.Fatalf("Execute() expected timeout while pool is at max size")
	}

	close(release)
	<-done
	if atomic.LoadInt32(&created) != 1 {
		t.Fatalf("created sessions = %d, want 1", created)
	}
}

func TestPoolHealthCheckDropsBadSessionAndMaintainsMinSize(t *testing.T) {
	var created int32
	restore := stubSessionFactory(func(context.Context) (*Session, error) {
		n := atomic.AddInt32(&created, 1)
		if n == 1 {
			return fakeSession(1, "bad"), nil
		}
		return fakeSession(0, "oculus-eks-health"), nil
	})
	defer restore()

	pool, err := NewPool("jumpserver-sg", "dev", PoolQuery, testJumpServer(), testEnv(), &conf.Pool{
		MinSize:             1,
		MaxSize:             1,
		HealthCheckInterval: durationPB(20 * time.Millisecond),
		HealthCheckTimeout:  durationPB(10 * time.Millisecond),
	}, nil)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Close()

	waitUntil(t, func() bool { return atomic.LoadInt32(&created) >= 2 && pool.Snapshot().Idle == 1 })
}

func TestPoolNewSessionUsesCanceledContext(t *testing.T) {
	original := dialContext
	dialCalled := make(chan context.Context, 1)
	dialContext = func(ctx context.Context, network string, address string, socks5Proxy string) (net.Conn, error) {
		dialCalled <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	}
	defer func() {
		dialContext = original
	}()

	pool, err := NewPool("jumpserver-sg", "dev", PoolQuery, &conf.JumpServer{
		Host:                  "jumpserver-sg.gainetics.io",
		User:                  "jeff",
		IdentityFile:          tempIdentityFile(t),
		InsecureIgnoreHostKey: true,
	}, testEnv(), &conf.Pool{
		MaxSize: 1,
	}, nil)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = pool.newSession(ctx)
	if err == nil {
		t.Fatalf("newSession() error = nil")
	}

	select {
	case gotCtx := <-dialCalled:
		if gotCtx.Err() == nil {
			t.Fatalf("dial context was not canceled")
		}
	default:
		t.Fatalf("dial was not called")
	}
}

func stubSessionFactory(factory func(context.Context) (*Session, error)) func() {
	original := newPoolSessionFactory
	newPoolSessionFactory = func(*Pool) func(context.Context) (*Session, error) {
		return factory
	}
	return func() {
		newPoolSessionFactory = original
	}
}

func fakeSession(exitCode int, output string) *Session {
	session := &Session{
		lastUsed: time.Now(),
		run: func(context.Context, string, time.Duration) (*CommandResult, error) {
			return &CommandResult{ExitCode: exitCode, Output: output}, nil
		},
	}
	session.cond = sync.NewCond(&session.mu)
	return session
}

func fakeSessionWithBlock(release <-chan struct{}) *Session {
	session := &Session{
		lastUsed: time.Now(),
		run: func(context.Context, string, time.Duration) (*CommandResult, error) {
			<-release
			return &CommandResult{ExitCode: 0, Output: "ok"}, nil
		},
	}
	session.cond = sync.NewCond(&session.mu)
	return session
}

func testJumpServer() *conf.JumpServer {
	return &conf.JumpServer{
		Host:         "jumpserver-sg.gainetics.io",
		User:         "jeff",
		IdentityFile: "~/.ssh/jump_rsa",
	}
}

func testEnv() *conf.EKSEnv {
	return &conf.EKSEnv{Name: "dev", Asset: "gainetics-eks-dev-cluster-admin"}
}

func waitUntil(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition was not met before deadline")
}

func durationPB(v time.Duration) *durationpb.Duration {
	return durationpb.New(v)
}

func tempIdentityFile(t *testing.T) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	key, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		t.Fatalf("MarshalPrivateKey() error = %v", err)
	}
	path := t.TempDir() + "/id_ed25519"
	if err := os.WriteFile(path, pem.EncodeToMemory(key), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
