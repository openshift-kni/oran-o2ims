/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func newISOTestSSHKey(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
}

func TestSeedGenerationSSHCommandChecksHostAndAuthentication(t *testing.T) {
	sshTempDir := t.TempDir()
	t.Setenv("SEED_GENERATION_SSH_TMP_DIR", sshTempDir)
	t.Setenv("TMPDIR", filepath.Join(sshTempDir, "missing"))

	host, _ := newISOTestSSHKey(t)
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaHost, err := ssh.NewSignerFromKey(ecdsaKey)
	if err != nil {
		t.Fatal(err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaHost, err := ssh.NewSignerFromKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	user, private := newISOTestSSHKey(t)
	wrongHost, _ := newISOTestSSHKey(t)
	_, wrongKey := newISOTestSSHKey(t)
	config := &ssh.ServerConfig{PublicKeyCallback: func(metadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if metadata.User() != "uploader" || !bytes.Equal(key.Marshal(), user.PublicKey().Marshal()) {
			return nil, fmt.Errorf("unauthorized key")
		}
		return &ssh.Permissions{}, nil
	}}
	config.AddHostKey(host)
	config.AddHostKey(ecdsaHost)
	config.AddHostKey(rsaHost)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			server, channels, requests, err := ssh.NewServerConn(connection, config)
			if err != nil {
				_ = connection.Close()
				continue
			}
			go ssh.DiscardRequests(requests)
			for channel := range channels {
				if channel.ChannelType() != "session" {
					_ = channel.Reject(ssh.UnknownChannelType, "unsupported")
					continue
				}
				session, requests, err := channel.Accept()
				if err != nil {
					continue
				}
				for request := range requests {
					if request.Type != "exec" {
						_ = request.Reply(false, nil)
						continue
					}
					_ = request.Reply(true, nil)
					var command struct{ Command string }
					status := uint32(0)
					if ssh.Unmarshal(request.Payload, &command) != nil || command.Command != "check" {
						status = 1
					}
					if status == 0 {
						_, _ = session.Write([]byte("checked"))
					}
					_, _ = session.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
					_ = session.Close()
					break
				}
			}
			_ = server.Close()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-stopped })
	address := listener.Addr().String()
	for _, tc := range []struct {
		name    string
		host    ssh.Signer
		key     []byte
		command string
		want    bool
		wantErr string
	}{
		{"valid ED25519 pin", host, private, "check", true, ""},
		{"valid ECDSA pin", ecdsaHost, private, "check", true, ""},
		{"valid RSA pin", rsaHost, private, "check", true, ""},
		{"wrong host key", wrongHost, private, "check", false, "SSH host key does not match known_hosts"},
		{"wrong user key", host, wrongKey, "check", false, "SSH handshake or authentication failed"},
		{"invalid key", host, []byte("invalid-private-key"), "check", false, "upload private key is invalid"},
		{"remote operation failed", host, private, "fail", false, "SSH filesystem operation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string][]byte{"username": []byte("uploader"), "privateKey": tc.key, "knownHosts": []byte(knownhosts.Line([]string{address}, tc.host.PublicKey()) + "\n")}
			output, err := runSeedGenerationSSHCommandAtAddress(context.Background(), address, data, tc.command)
			if tc.want {
				if err != nil || string(output) != "checked" {
					t.Fatalf("SSH check failed: %s %v", output, err)
				}
			} else if err == nil {
				t.Fatal("invalid SSH operation succeeded")
			}
			if tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected SSH failure: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), string(tc.key)) {
				t.Fatal("SSH failure exposed private key")
			}
		})
	}
	t.Run("hashed host pin with unrelated key", func(t *testing.T) {
		pinnedHost := knownhosts.HashHostname(knownhosts.Normalize(address))
		data := map[string][]byte{
			"username":   []byte("uploader"),
			"privateKey": private,
			"knownHosts": []byte(knownhosts.Line([]string{"other.example.com"}, ecdsaHost.PublicKey()) + "\n" +
				knownhosts.Line([]string{pinnedHost}, host.PublicKey()) + "\n"),
		}
		output, err := runSeedGenerationSSHCommandAtAddress(context.Background(), address, data, "check")
		if err != nil || string(output) != "checked" {
			t.Fatalf("SSH check with hashed host pin failed: %s %v", output, err)
		}
	})
}

func TestSeedGenerationSSHCommandReportsTempDirectoryFailure(t *testing.T) {
	t.Setenv("SEED_GENERATION_SSH_TMP_DIR", filepath.Join(t.TempDir(), "missing"))
	host, private := newISOTestSSHKey(t)
	data := map[string][]byte{
		"username":   []byte("uploader"),
		"privateKey": private,
		"knownHosts": []byte(knownhosts.Line([]string{"example.test"}, host.PublicKey()) + "\n"),
	}
	_, err := runSeedGenerationSSHCommandAtAddress(context.Background(), "example.test:22", data, "check")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected temporary directory failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "failed to prepare SSH known-host verification") {
		t.Fatalf("unexpected SSH preparation error: %v", err)
	}
	if strings.Contains(err.Error(), string(private)) {
		t.Fatal("SSH preparation error exposed private key")
	}
}

func TestSeedGenerationSSHOutputIsBounded(t *testing.T) {
	output := &seedGenerationSSHOutput{}
	data := []byte(strings.Repeat("a", 16<<10))
	count, err := output.Write(data)
	if err != nil || count != len(data) || output.Len() != 4<<10 {
		t.Fatalf("unexpected output bound: len=%d count=%d err=%v", output.Len(), count, err)
	}
	output = &seedGenerationSSHOutput{}
	if _, err := io.Copy(output, io.LimitReader(strings.NewReader(string(data)), int64(len(data)))); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 4<<10 {
		t.Fatalf("io.Copy bypassed the output bound: %d", output.Len())
	}
}
