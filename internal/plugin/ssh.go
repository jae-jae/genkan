package plugin

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/MoeForge/moejs"
	"golang.org/x/crypto/ssh"
)

// No shell interpolation, configurable commands, local processes or remote writes.
const sshStatsCommand = "LC_ALL=C /bin/sh -s"
const sshStatsScript = `set -eu
PATH=/usr/bin:/bin:/usr/sbin:/sbin
export PATH LC_ALL
[ "$(uname -s)" = Linux ]
printf '\n@gk:host\n'; uname -n
printf '\n@gk:kernel\n'; uname -r
printf '\n@gk:arch\n'; uname -m
printf '\n@gk:cores\n'; awk '/^cpu[0-9]+ / {n++} END {print n}' /proc/stat
printf '\n@gk:cpu1\n'; awk '/^cpu / {print; exit}' /proc/stat
printf '\n@gk:net1\n'; cat /proc/net/dev
printf '\n@gk:time1\n'; cat /proc/uptime
sleep 1
printf '\n@gk:cpu2\n'; awk '/^cpu / {print; exit}' /proc/stat
printf '\n@gk:net2\n'; cat /proc/net/dev
printf '\n@gk:time2\n'; cat /proc/uptime
printf '\n@gk:memory\n'; cat /proc/meminfo
printf '\n@gk:load\n'; cat /proc/loadavg
printf '\n@gk:disk\n'; df -Pk / | tail -n 1
`

const sshOutputLimit = 64 << 10

var sshSlots = make(chan struct{}, 8)

type sshOptions struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	PrivateKey  string `json:"privateKey"`
	Passphrase  string `json:"passphrase"`
	Fingerprint string `json:"fingerprint"`
}

func (in *instance) sshStats(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	if in.loading.Load() || in.hookCtx == nil {
		return moejs.Undefined(), errNoHost
	}
	if in.e.man == nil || !in.e.man.Permissions.SSH {
		return moejs.Undefined(), errors.New("permission denied: genkan.ssh.stats needs permissions.ssh = true")
	}
	if in.sshCalls >= 1 {
		return moejs.Undefined(), errors.New("SSH_LIMIT")
	}
	in.sshCalls++
	g, err := in.rt.ToGoWith(moejs.Arg(args, 0), moejs.DecodeOptions{MaxBytes: 16 << 10})
	if err != nil {
		return moejs.Undefined(), errors.New("SSH_CONFIG")
	}
	if _, ok := g.(map[string]any); !ok {
		return moejs.Undefined(), errors.New("SSH_CONFIG")
	}
	b, err := json.Marshal(g)
	if err != nil {
		return moejs.Undefined(), errors.New("SSH_CONFIG")
	}
	var opts sshOptions
	if json.Unmarshal(b, &opts) != nil {
		return moejs.Undefined(), errors.New("SSH_CONFIG")
	}
	ctx := in.hookCtx
	promise, settle := in.loop.NewPromise()
	go func() {
		data, err := collectSSH(ctx, opts)
		settle(func(rt *moejs.Runtime) (moejs.Value, error) {
			if err != nil {
				return moejs.Undefined(), err
			}
			return rt.FromGo(data)
		})
	}()
	return promise, nil
}

func validateSSH(o *sshOptions) error {
	if o.Port == 0 {
		o.Port = 22
	}
	if o.Port < 1 || o.Port > 65535 || o.Host == "" || len(o.Host) > 253 || o.Username == "" || len(o.Username) > 128 {
		return errors.New("SSH_CONFIG")
	}
	for _, s := range []string{o.Host, o.Username} {
		if strings.ContainsAny(s, "/\\@?#") || strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return errors.New("SSH_CONFIG")
		}
	}
	if strings.ContainsAny(o.Host, "[]") || (strings.Contains(o.Host, ":") && net.ParseIP(o.Host) == nil) {
		return errors.New("SSH_CONFIG")
	}
	if o.Fingerprint != "" {
		fp, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(o.Fingerprint, "SHA256:"))
		if !strings.HasPrefix(o.Fingerprint, "SHA256:") || err != nil || len(fp) != 32 {
			return errors.New("SSH_FINGERPRINT")
		}
	}
	if (o.Password == "") == (o.PrivateKey == "") || len(o.PrivateKey) > 5000 || len(o.Password) > 2000 || len(o.Passphrase) > 2000 {
		return errors.New("SSH_CREDENTIALS")
	}
	return nil
}

// All errors are stable codes. Never return server banners, stderr or library
// errors: these may contain credentials or attacker-controlled text.
func collectSSH(parent context.Context, o sshOptions) (map[string]any, error) {
	if err := validateSSH(&o); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 7*time.Second)
	defer cancel()
	select {
	case sshSlots <- struct{}{}:
		defer func() { <-sshSlots }()
	case <-ctx.Done():
		return nil, errors.New("SSH_TIMEOUT")
	}
	var auth ssh.AuthMethod
	if o.PrivateKey != "" {
		var signer ssh.Signer
		var err error
		if o.Passphrase != "" {
			if err := checkSSHKeyCost([]byte(o.PrivateKey)); err != nil {
				return nil, err
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(o.PrivateKey), []byte(o.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(o.PrivateKey))
		}
		if err != nil {
			return nil, errors.New("SSH_KEY")
		}
		auth = ssh.PublicKeys(signer)
	} else {
		auth = ssh.Password(o.Password)
	}
	address := net.JoinHostPort(o.Host, strconv.Itoa(o.Port))
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("SSH_TIMEOUT")
		}
		return nil, errors.New("SSH_CONNECT")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	timedOut := func() bool { return ctx.Err() != nil || !time.Now().Before(deadline) }
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, errors.New("SSH_CONNECT")
	}
	mismatch := false
	config := &ssh.ClientConfig{User: o.Username, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		// An omitted fingerprint explicitly skips server identity verification.
		if o.Fingerprint != "" && subtle.ConstantTimeCompare([]byte(ssh.FingerprintSHA256(key)), []byte(o.Fingerprint)) != 1 {
			mismatch = true
			return errors.New("SSH_HOST_KEY")
		}
		return nil
	}}
	c, channels, requests, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		if mismatch {
			return nil, errors.New("SSH_HOST_KEY")
		}
		if timedOut() {
			return nil, errors.New("SSH_TIMEOUT")
		}
		return nil, errors.New("SSH_HANDSHAKE")
	}
	client := ssh.NewClient(c, channels, requests)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		if timedOut() {
			return nil, errors.New("SSH_TIMEOUT")
		}
		return nil, errors.New("SSH_SESSION")
	}
	defer session.Close()
	out := &sshBoundedWriter{limit: sshOutputLimit}
	stderr := &sshBoundedWriter{limit: 8192}
	session.Stdout, session.Stderr, session.Stdin = out, stderr, strings.NewReader(sshStatsScript)
	err = session.Run(sshStatsCommand)
	if timedOut() {
		return nil, errors.New("SSH_TIMEOUT")
	}
	if out.over || stderr.over {
		return nil, errors.New("SSH_OUTPUT")
	}
	if err != nil {
		return nil, errors.New("SSH_COLLECT")
	}
	result, err := parseSSHStats(out.String())
	if err != nil {
		return nil, errors.New("SSH_DATA")
	}
	result["collectedAt"] = time.Now().UnixMilli()
	return result, nil
}

type sshBoundedWriter struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (w *sshBoundedWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.buf.Len() {
		w.over = true
		return 0, io.ErrShortBuffer
	}
	return w.buf.Write(p)
}

func (w *sshBoundedWriter) String() string { return w.buf.String() }

// bcrypt private-key decoding does CPU work outside the cancellable transport.
// Bound its cost below the library's general-purpose limit (2048 rounds).
func checkSSHKeyCost(key []byte) error {
	block, _ := pem.Decode(key)
	if block == nil {
		return errors.New("SSH_KEY")
	}
	if block.Type != "OPENSSH PRIVATE KEY" {
		return nil
	}
	const magic = "openssh-key-v1\x00"
	if !bytes.HasPrefix(block.Bytes, []byte(magic)) {
		return errors.New("SSH_KEY")
	}
	var header struct {
		Cipher, KDF, Options string
		Rest                 []byte `ssh:"rest"`
	}
	if ssh.Unmarshal(block.Bytes[len(magic):], &header) != nil {
		return errors.New("SSH_KEY")
	}
	if header.KDF != "bcrypt" {
		return nil
	} // The SSH library validates other formats.
	var opts struct {
		Salt   string
		Rounds uint32
	}
	if ssh.Unmarshal([]byte(header.Options), &opts) != nil {
		return errors.New("SSH_KEY")
	}
	if opts.Rounds > 64 || len(opts.Salt) > 64 {
		return errors.New("SSH_KEY_COST")
	}
	return nil
}
