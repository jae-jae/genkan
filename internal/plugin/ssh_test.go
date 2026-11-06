package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

const sshFixture = `
@gk:host
lab-server
@gk:kernel
6.8.0
@gk:arch
x86_64
@gk:cores
4
@gk:cpu1
cpu 100 0 50 800 50 0 0 0 10 0
@gk:cpu2
cpu 120 0 60 860 60 0 0 0 20 0
@gk:time1
1000.00 4000.00
@gk:time2
1001.00 4004.00
@gk:net1
Inter-| Receive | Transmit
 face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed
 eth0: 1000 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0
 lo: 99999 0 0 0 0 0 0 0 99999 0 0 0 0 0 0 0
@gk:net2
Inter-| Receive | Transmit
 face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed
 eth0: 3048 0 0 0 0 0 0 0 1524 0 0 0 0 0 0 0
 lo: 999999 0 0 0 0 0 0 0 999999 0 0 0 0 0 0 0
@gk:memory
MemTotal: 8192 kB
MemAvailable: 2048 kB
SwapTotal: 0 kB
SwapFree: 0 kB
@gk:load
0.25 0.50 0.75 1/100 99
@gk:disk
/dev/sda1 1000 400 500 45% /
`

func TestSSHParse(t *testing.T) {
	d, err := parseSSHStats(sshFixture)
	if err != nil {
		t.Fatal(err)
	}
	if d["cpuPercent"].(float64) < 29.99 || d["cpuPercent"].(float64) > 30.01 {
		t.Fatalf("CPU includes guest time: %v", d)
	}
	if d["memory"].(map[string]any)["percent"] != float64(75) {
		t.Fatal(d)
	}
	if d["network"].(map[string]any)["rxBytesPerSecond"] != float64(2048) {
		t.Fatal(d)
	}
	if d["network"].(map[string]any)["txBytesPerSecond"] != float64(1024) {
		t.Fatal(d)
	}
	for _, raw := range []string{"", strings.ReplaceAll(sshFixture, "MemAvailable: 2048", "MemAvailable: 99999"), strings.ReplaceAll(sshFixture, "1001.00", "999.00"), strings.ReplaceAll(sshFixture, "1001.00", "NaN"), strings.ReplaceAll(sshFixture, "cpu 120", "cpu -120"), strings.ReplaceAll(sshFixture, "/dev/sda1 1000", "/dev/sda1 0"), sshFixture + "\n@gk:host\nother"} {
		if _, err := parseSSHStats(raw); err == nil {
			t.Error("accepted invalid snapshot")
		}
	}
	reset := strings.ReplaceAll(sshFixture, "eth0: 3048", "eth0: 10")
	d, err = parseSSHStats(reset)
	if err != nil || d["network"].(map[string]any)["rxBytesPerSecond"] != float64(0) {
		t.Fatalf("counter reset: %v %v", d, err)
	}
}

// Real SSH handshake, authentication, session and exec requests, without using
// the developer's SSH config, keys, network hosts or remote shell.
func testSSHServer(t *testing.T, output string, stall bool) (sshOptions, *atomic.Int32, string) {
	t.Helper()
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(private)
	block, _ := ssh.MarshalPrivateKey(private, "test")
	key := string(pem.EncodeToMemory(block))
	authenticated := &atomic.Int32{}
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			authenticated.Add(1)
			if c.User() != "monitor" || string(p) != "test-password" {
				return nil, fmt.Errorf("rejected")
			}
			return nil, nil
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() != "monitor" || string(k.Marshal()) != string(signer.PublicKey().Marshal()) {
				return nil, fmt.Errorf("rejected")
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				server, chans, reqs, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					if ch.ChannelType() != "session" {
						_ = ch.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					channel, requests, err := ch.Accept()
					if err != nil {
						return
					}
					for req := range requests {
						if req.Type != "exec" {
							_ = req.Reply(false, nil)
							continue
						}
						var cmd struct{ Command string }
						_ = ssh.Unmarshal(req.Payload, &cmd)
						if cmd.Command != sshStatsCommand {
							t.Errorf("unexpected command %s", cmd.Command)
						}
						_ = req.Reply(true, nil)
						script, _ := io.ReadAll(io.LimitReader(channel, 8192))
						if string(script) != sshStatsScript {
							t.Error("unexpected stdin script")
						}
						if stall {
							<-done
							_ = channel.Close()
							return
						}
						if output == "__linux__" {
							cmd := exec.Command("/bin/sh", "-s")
							cmd.Stdin = strings.NewReader(sshStatsScript)
							cmd.Stdout, cmd.Stderr = channel, channel.Stderr()
							if err := cmd.Run(); err != nil {
								t.Errorf("Linux script: %v", err)
							}
						} else {
							_, _ = io.WriteString(channel, output)
						}
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						_ = channel.Close()
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { close(done); listener.Close(); wg.Wait() })
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	var n int
	_, _ = fmt.Sscan(port, &n)
	return sshOptions{Host: host, Port: n, Username: "monitor", Password: "test-password", Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}, authenticated, key
}

func TestSSHCollect(t *testing.T) {
	opts, auth, key := testSSHServer(t, sshFixture, false)
	d, err := collectSSH(context.Background(), opts)
	if err != nil || d["hostname"] != "lab-server" {
		t.Fatalf("collect %v %v", d, err)
	}
	bad := opts
	bad.Fingerprint = "SHA256:" + strings.Repeat("A", 43)
	before := auth.Load()
	if _, err := collectSSH(context.Background(), bad); err == nil || err.Error() != "SSH_HOST_KEY" {
		t.Fatalf("fingerprint: %v", err)
	}
	if auth.Load() != before {
		t.Fatal("sent credentials before host verification")
	}
	bad = opts
	bad.Password = "wrong-password"
	if _, err := collectSSH(context.Background(), bad); err == nil || err.Error() != "SSH_HANDSHAKE" {
		t.Fatalf("auth: %v", err)
	}
	opts.Password = ""
	opts.PrivateKey = key
	if _, err := collectSSH(context.Background(), opts); err != nil {
		t.Fatalf("public key: %v", err)
	}
}

func TestSSHCollectWithoutFingerprint(t *testing.T) {
	opts, _, key := testSSHServer(t, sshFixture, false)
	opts.Fingerprint = ""
	for _, method := range []string{"password", "privateKey"} {
		t.Run(method, func(t *testing.T) {
			o := opts
			if method == "privateKey" {
				o.Password, o.PrivateKey = "", key
			}
			d, err := collectSSH(context.Background(), o)
			if err != nil || d["hostname"] != "lab-server" {
				t.Fatalf("collect without fingerprint: %v %v", d, err)
			}
			o.Password, o.PrivateKey = "wrong-password", ""
			if _, err := collectSSH(context.Background(), o); err == nil || err.Error() != "SSH_HANDSHAKE" {
				t.Fatalf("authentication must still be required: %v", err)
			}
		})
	}
}

func TestSSHCancelAndOutputLimit(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		opts, _, _ := testSSHServer(t, sshFixture, true)
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := collectSSH(ctx, opts)
		if err == nil || err.Error() != "SSH_TIMEOUT" || time.Since(start) > time.Second {
			t.Fatalf("cancel: %v", err)
		}
	})
	t.Run("large", func(t *testing.T) {
		opts, _, _ := testSSHServer(t, strings.Repeat("X", sshOutputLimit+1), false)
		if _, err := collectSSH(context.Background(), opts); err == nil || err.Error() != "SSH_OUTPUT" {
			t.Fatalf("limit: %v", err)
		}
	})
}

func TestSSHRuntimePermissions(t *testing.T) {
	e := newEnv(t)
	body := `export async function widget(){const d=await genkan.ssh.stats({});return {body:[]}}`
	e.install(plug(`{id:"ssh-denied",name:"SSH",version:"1",widgets:[{id:"main",name:"M"}]}`, body))
	r, err := e.m.Test(context.Background(), "ssh-denied", TestRequest{Hook: "widget"})
	if err != nil || r.OK || !strings.Contains(r.Error, "permissions.ssh") {
		t.Fatalf("deny: %+v", r)
	}
	opts, _, _ := testSSHServer(t, sshFixture, false)
	b, _ := json.Marshal(opts)
	e.install(plug(`{id:"ssh-allowed",name:"SSH",version:"1",permissions:{ssh:true},widgets:[{id:"main",name:"M"}]}`, `export async function widget(){const d=await genkan.ssh.stats(`+string(b)+`);return {body:[{type:"text",text:d.hostname}]}}`))
	r, err = e.m.Test(context.Background(), "ssh-allowed", TestRequest{Hook: "widget"})
	if err != nil || !r.OK || !strings.Contains(fmt.Sprint(r.Result), "lab-server") {
		t.Fatalf("allow: %+v", r)
	}
	if err := e.installErr(plug(`{id:"ssh-invalid",name:"SSH",version:"1",permissions:{ssh:"yes"},widgets:[{id:"main",name:"M"}]}`, body)); !strings.Contains(err.Error(), "permissions.ssh") {
		t.Fatal(err)
	}
}

func TestSSHMultilineSecret(t *testing.T) {
	fields, err := parseFields([]any{map[string]any{"key": "key", "type": "secret", "multiline": true, "label": "Key"}}, "config")
	if err != nil {
		t.Fatal(err)
	}
	key := "-----BEGIN OPENSSH PRIVATE KEY-----\n" + strings.Repeat("a", 3000) + "\n-----END OPENSSH PRIVATE KEY-----"
	cfg, err := cleanConfig(fields, map[string]any{"key": key}, nil, true, "en")
	if err != nil {
		t.Fatal(err)
	}
	masked, keys := maskConfig(fields, cfg, true)
	if masked["key"] != "" || len(keys) != 1 {
		t.Fatalf("secret leak: %v", keys)
	}
	kept, err := cleanConfig(fields, map[string]any{}, cfg, true, "en")
	if err != nil || kept["key"] != key {
		t.Fatal("secret not retained")
	}
	fields[0].Multiline = false
	if _, err := checkValue(&fields[0], key); err == nil {
		t.Error("ordinary secret accepted multiline")
	}
}

func TestSSHCollectLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /proc; also exercised with the cross-compiled test binary in Docker")
	}
	opts, _, _ := testSSHServer(t, "__linux__", false)
	d, err := collectSSH(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if d["cores"].(float64) < 1 || d["hostname"] == "" {
		t.Fatalf("missing real metrics: %v", d)
	}
}

func TestSSHValidation(t *testing.T) {
	base := sshOptions{Host: "server.example", Username: "monitor", Password: "test-password", Fingerprint: "SHA256:" + strings.Repeat("A", 43)}
	for _, change := range []func(*sshOptions){
		func(o *sshOptions) { o.Host = "server; echo injected" }, func(o *sshOptions) { o.Host = "host:22" },
		func(o *sshOptions) { o.Port = 65536 }, func(o *sshOptions) { o.Port = -1 }, func(o *sshOptions) { o.Fingerprint = "invalid" },
		func(o *sshOptions) { o.PrivateKey = "key" }, func(o *sshOptions) { o.Password = "" }, func(o *sshOptions) { o.Username = "bad\nuser" },
	} {
		o := base
		change(&o)
		if validateSSH(&o) == nil {
			t.Error("invalid configuration accepted")
		}
	}
	base.Host = "::1"
	if err := validateSSH(&base); err != nil {
		t.Fatalf("IPv6: %v", err)
	}
}

func TestSSHRuntimeLimitsAndInstallGuard(t *testing.T) {
	e := newEnv(t)
	manifest := `{id:"ssh-limits",name:"SSH",version:"1",permissions:{ssh:true},widgets:[{id:"main",name:"M"}]}`
	body := `export async function widget(){try { await genkan.ssh.stats({}); } catch (_) {} await genkan.ssh.stats({}); return {body:[]}}`
	e.install(plug(manifest, body))
	r, err := e.m.Test(context.Background(), "ssh-limits", TestRequest{Hook: "widget"})
	if err != nil || r.OK || !strings.Contains(r.Error, "SSH_LIMIT") {
		t.Fatalf("quota: %+v %v", r, err)
	}
	if err := e.installErr(plug(manifest, `genkan.ssh.stats({}); export async function widget(){return {body:[]}}`)); !strings.Contains(err.Error(), "not available") {
		t.Fatal(err)
	}
}

func TestSSHEncryptedKey(t *testing.T) {
	opts, _, key := testSSHServer(t, sshFixture, false)
	private, err := ssh.ParseRawPrivateKey([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := ssh.MarshalPrivateKeyWithPassphrase(private, "test", []byte("test-passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	opts.Password = ""
	opts.PrivateKey = string(pem.EncodeToMemory(encrypted))
	opts.Passphrase = "test-passphrase"
	if _, err := collectSSH(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	opts.Passphrase = "wrong"
	if _, err := collectSSH(context.Background(), opts); err == nil || err.Error() != "SSH_KEY" {
		t.Fatalf("passphrase: %v", err)
	}
	kdf := ssh.Marshal(struct {
		Salt   string
		Rounds uint32
	}{"salt", 1000000})
	header := ssh.Marshal(struct{ Cipher, KDF, Options string }{"aes256-ctr", "bcrypt", string(kdf)})
	bomb := pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: append([]byte("openssh-key-v1\x00"), header...)})
	if err := checkSSHKeyCost(bomb); err == nil || err.Error() != "SSH_KEY_COST" {
		t.Fatalf("KDF cost: %v", err)
	}
}
