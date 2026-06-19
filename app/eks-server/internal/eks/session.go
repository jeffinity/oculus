package eks

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/oklog/ulid/v2"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

const defaultMaxCommandOutput = 512 * 1024

type SessionOptions struct {
	Cluster        string
	Env            string
	Pool           PoolKind
	Host           string
	Port           uint32
	User           string
	IdentityFile   string
	PassphraseEnv  string
	SOCKS5Proxy    string
	IgnoreHostKey  bool
	Asset          string
	OptPrompt      string
	ShellPrompt    string
	ConnectTimeout time.Duration
	Logger         *log.Helper
}

var dialContext = func(ctx context.Context, network string, address string, socks5Proxy string) (net.Conn, error) {
	if socks5Proxy == "" {
		var d net.Dialer
		return d.DialContext(ctx, network, address)
	}

	base := &contextDialer{ctx: ctx}
	socksDialer, err := proxy.SOCKS5(network, socks5Proxy, nil, base)
	if err != nil {
		return nil, errors.WithMessage(err, "create socks5 dialer")
	}
	return socksDialer.Dial(network, address)
}

type contextDialer struct {
	ctx context.Context
}

func (d *contextDialer) Dial(network string, address string) (net.Conn, error) {
	var nd net.Dialer
	return nd.DialContext(d.ctx, network, address)
}

type Session struct {
	opts SessionOptions
	conn *ssh.Client
	sess *ssh.Session
	in   io.WriteCloser
	run  func(context.Context, string, time.Duration) (*CommandResult, error)

	mu       sync.Mutex
	cond     *sync.Cond
	buffer   strings.Builder
	closed   bool
	lastUsed time.Time
}

func NewSession(ctx context.Context, opts SessionOptions) (*Session, error) {
	if opts.OptPrompt == "" {
		opts.OptPrompt = "Opt>"
	}
	if opts.ShellPrompt == "" {
		opts.ShellPrompt = "#"
	}

	conn, sess, in, out, err := newSSHSession(ctx, opts)
	if err != nil {
		return nil, err
	}

	s := &Session{
		opts:     opts,
		conn:     conn,
		sess:     sess,
		in:       in,
		lastUsed: time.Now(),
	}
	s.cond = sync.NewCond(&s.mu)
	go s.readLoop(out)
	go s.waitLoop()

	connectCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()

	if err := s.waitFor(connectCtx, opts.OptPrompt); err != nil {
		_ = s.Close()
		return nil, errors.WithMessage(err, "wait for jumpserver option prompt")
	}
	if err := s.writeLine(opts.Asset); err != nil {
		_ = s.Close()
		return nil, errors.WithMessage(err, "select jumpserver asset")
	}
	if err := s.waitFor(connectCtx, opts.ShellPrompt); err != nil {
		_ = s.Close()
		return nil, errors.WithMessage(err, "wait for cluster shell prompt")
	}
	s.clearBuffer()
	return s, nil
}

func (s *Session) Run(ctx context.Context, command string, timeout time.Duration) (*CommandResult, error) {
	if s.run != nil {
		s.touch()
		return s.run(ctx, command, timeout)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return &CommandResult{ExitCode: -1}, errors.New("session is closed")
	}
	s.buffer.Reset()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	marker := "__OCULUS_EKS_" + strings.ToUpper(ulid.Make().String()) + "__"
	wrapped := fmt.Sprintf("printf '%s_BEGIN\\n'; %s; __oculus_rc=$?; printf '%s_END:%%s\\n%s_DONE\\n' \"$__oculus_rc\"", marker, command, marker, marker)

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := s.writeLine(wrapped); err != nil {
		return &CommandResult{ExitCode: -1}, err
	}
	if err := s.waitFor(cmdCtx, marker+"_DONE"); err != nil {
		return &CommandResult{ExitCode: -1, Output: s.snapshot()}, err
	}

	raw := s.snapshot()
	s.touch()
	return parseMarkedOutput(raw, marker)
}

func (s *Session) IsAlive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

func (s *Session) IsExpired(idleTTL time.Duration) bool {
	if idleTTL <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastUsed) > idleTTL
}

func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.cond != nil {
		s.cond.Broadcast()
	}
	s.mu.Unlock()

	if s.in != nil {
		_, _ = s.in.Write([]byte("exit\n"))
		_ = s.in.Close()
	}
	if s.sess != nil {
		_ = s.sess.Close()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	return nil
}

func (s *Session) touch() {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

func (s *Session) writeLine(line string) error {
	_, err := s.in.Write([]byte(line + "\n"))
	return errors.WithStack(err)
}

func (s *Session) waitFor(ctx context.Context, needle string) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		s.mu.Lock()
		found := strings.Contains(s.buffer.String(), needle)
		closed := s.closed
		s.mu.Unlock()
		if found {
			return nil
		}
		if closed {
			return errors.New("session closed")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Session) snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buffer.String()
}

func (s *Session) clearBuffer() {
	s.mu.Lock()
	s.buffer.Reset()
	s.mu.Unlock()
}

func (s *Session) readLoop(out io.Reader) {
	reader := bufio.NewReader(out)
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			s.mu.Lock()
			_, _ = s.buffer.Write(buf[:n])
			if s.buffer.Len() > defaultMaxCommandOutput {
				current := s.buffer.String()
				s.buffer.Reset()
				s.buffer.WriteString(current[len(current)-defaultMaxCommandOutput:])
			}
			s.cond.Broadcast()
			s.mu.Unlock()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && s.opts.Logger != nil {
				s.opts.Logger.Debugf("eks session read stopped: %+v", err)
			}
			s.mu.Lock()
			s.closed = true
			s.cond.Broadcast()
			s.mu.Unlock()
			return
		}
	}
}

func (s *Session) waitLoop() {
	err := s.sess.Wait()
	if err != nil && s.opts.Logger != nil {
		s.opts.Logger.Debugf("eks session exited: %+v", err)
	}
	s.mu.Lock()
	s.closed = true
	s.cond.Broadcast()
	s.mu.Unlock()
}

func newSSHSession(ctx context.Context, opts SessionOptions) (*ssh.Client, *ssh.Session, io.WriteCloser, io.Reader, error) {
	clientConfig, err := buildSSHClientConfig(opts)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	addr := net.JoinHostPort(opts.Host, strconv.Itoa(int(portOrDefault(opts.Port))))
	tcpConn, err := dialJumpServer(ctx, addr, opts.SOCKS5Proxy)
	if err != nil {
		return nil, nil, nil, nil, errors.WithMessage(err, "dial jumpserver")
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, clientConfig)
	if err != nil {
		_ = tcpConn.Close()
		return nil, nil, nil, nil, errors.WithMessage(err, "ssh handshake")
	}
	client := ssh.NewClient(sshConn, chans, reqs)

	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, nil, nil, nil, errors.WithMessage(err, "new ssh session")
	}

	in, outReader, err := openShellPipes(session)
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, nil, nil, nil, err
	}
	if err := startShell(session); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, nil, nil, nil, err
	}

	return client, session, in, outReader, nil
}

func openShellPipes(session *ssh.Session) (io.WriteCloser, io.Reader, error) {
	in, err := session.StdinPipe()
	if err != nil {
		return nil, nil, errors.WithMessage(err, "open ssh stdin")
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, nil, errors.WithMessage(err, "open ssh stdout")
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return nil, nil, errors.WithMessage(err, "open ssh stderr")
	}
	return in, mergeReaders(stdout, stderr), nil
}

func startShell(session *ssh.Session) error {
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm", 40, 120, modes); err != nil {
		return errors.WithMessage(err, "request ssh pty")
	}
	if err := session.Shell(); err != nil {
		return errors.WithMessage(err, "start ssh shell")
	}
	return nil
}

func mergeReaders(readers ...io.Reader) io.Reader {
	reader, writer := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(len(readers))
	for _, src := range readers {
		go func() {
			defer wg.Done()
			_, _ = io.Copy(writer, src)
		}()
	}
	go func() {
		wg.Wait()
		_ = writer.Close()
	}()
	return reader
}

func buildSSHClientConfig(opts SessionOptions) (*ssh.ClientConfig, error) {
	if opts.Host == "" {
		return nil, errors.New("jumpserver host is required")
	}
	if opts.User == "" {
		return nil, errors.New("jumpserver user is required")
	}
	if opts.IdentityFile == "" {
		return nil, errors.New("jumpserver identity_file is required")
	}

	signer, err := loadPrivateKey(opts.IdentityFile, opts.PassphraseEnv)
	if err != nil {
		return nil, err
	}
	hostKeyCallback := ssh.InsecureIgnoreHostKey()
	if !opts.IgnoreHostKey {
		return nil, errors.New("jumpserver host key verification is not configured; set insecure_ignore_host_key=true")
	}

	return &ssh.ClientConfig{
		User:            opts.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         opts.ConnectTimeout,
	}, nil
}

func loadPrivateKey(identityFile string, passphraseEnv string) (ssh.Signer, error) {
	path, err := expandPath(identityFile)
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.WithMessagef(err, "read identity file %s", path)
	}
	if passphraseEnv != "" {
		passphrase := os.Getenv(passphraseEnv)
		if passphrase == "" {
			return nil, fmt.Errorf("identity passphrase env %s is empty", passphraseEnv)
		}
		signer, err := ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
		return signer, errors.WithMessage(err, "parse encrypted identity file")
	}
	signer, err := ssh.ParsePrivateKey(key)
	return signer, errors.WithMessage(err, "parse identity file")
}

func dialJumpServer(ctx context.Context, addr string, socks5Proxy string) (net.Conn, error) {
	return dialContext(ctx, "tcp", addr, socks5Proxy)
}

func expandPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.WithStack(err)
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.WithStack(err)
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func portOrDefault(port uint32) uint32 {
	if port == 0 {
		return 22
	}
	return port
}

func parseMarkedOutput(raw string, marker string) (*CommandResult, error) {
	clean := normalizePTY(raw)
	begin := marker + "_BEGIN"
	end := marker + "_END:"

	beginAt := strings.Index(clean, begin)
	endAt := strings.LastIndex(clean, end)
	if beginAt < 0 || endAt < 0 || endAt < beginAt {
		return &CommandResult{ExitCode: -1, Output: clean}, errors.New("command markers not found")
	}

	outputStart := beginAt + len(begin)
	output := clean[outputStart:endAt]
	output = strings.Trim(output, "\n")

	rcLine := clean[endAt+len(end):]
	rcLine = strings.TrimSpace(firstLine(rcLine))
	exitCode, err := strconv.Atoi(rcLine)
	if err != nil {
		return &CommandResult{ExitCode: -1, Output: output}, errors.WithMessage(err, "parse command exit code")
	}

	return &CommandResult{
		Output:   output,
		ExitCode: exitCode,
	}, nil
}

func normalizePTY(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	var buf bytes.Buffer
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "printf ") {
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return buf.String()
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
