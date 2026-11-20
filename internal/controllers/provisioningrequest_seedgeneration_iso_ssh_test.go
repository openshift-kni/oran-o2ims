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
	"os"
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

func isoTestSSHUnsupportedHostEntry(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/seedgeneration-dsa-known-hosts")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
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
	pin := func(signer ssh.Signer) string {
		return knownhosts.Line([]string{address}, signer.PublicKey()) + "\n"
	}
	unsupportedEntry := isoTestSSHUnsupportedHostEntry(t)
	revokedEntries := "@revoked " + pin(host) + "@revoked " + pin(ecdsaHost) + "@revoked " + pin(rsaHost)
	for _, tc := range []struct {
		name       string
		knownHosts string
		key        []byte
		command    string
		want       bool
		wantErr    string
	}{
		{"valid ED25519 pin", pin(host), private, "check", true, ""},
		{"valid ECDSA pin", pin(ecdsaHost), private, "check", true, ""},
		{"valid RSA pin", pin(rsaHost), private, "check", true, ""},
		{"wrong host key", pin(wrongHost), private, "check", false, "SSH host key does not match known_hosts"},
		{"wrong user key", pin(host), wrongKey, "check", false, "SSH handshake or authentication failed"},
		{"invalid key", pin(host), []byte("invalid-private-key"), "check", false, "upload private key is invalid"},
		{"remote operation failed", pin(host), private, "fail", false, "SSH filesystem operation"},
		{"unsupported entry before pin", unsupportedEntry + "\n" + pin(host), private, "check", true, ""},
		{"unsupported entry after pin", pin(host) + unsupportedEntry + "\n", private, "check", true, ""},
		{"unsupported entry before pin without final newline", unsupportedEntry + "\n" + strings.TrimSuffix(pin(host), "\n"), private, "check", true, ""},
		{"unsupported entry after pin without final newline", pin(host) + unsupportedEntry, private, "check", true, ""},
		{"malformed entry before pin", "invalid entry\n" + pin(host), private, "check", true, ""},
		{"unsupported entry only", unsupportedEntry, private, "check", false, "known_hosts data for SSH is invalid"},
		{"malformed entry only", "invalid entry\n", private, "check", false, "known_hosts data for SSH is invalid"},
		{"unsupported entry with unpinned host", unsupportedEntry + "\n" + knownhosts.Line([]string{"other.example.com"}, host.PublicKey()), private, "check", false, "SSH host key is not pinned"},
		{"unsupported entry with mismatched pin", unsupportedEntry + "\n" + pin(wrongHost), private, "check", false, "SSH host key does not match known_hosts"},
		{"unsupported entry with revoked keys", unsupportedEntry + "\n" + pin(host) + revokedEntries, private, "check", false, "SSH host key is revoked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string][]byte{"username": []byte("uploader"), "privateKey": tc.key, "knownHosts": []byte(tc.knownHosts)}
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

func TestSeedGenerationSSHHostKeyAlgorithmsPreservesCertificateAuthorities(t *testing.T) {
	const address = "upload.example.test:22"
	remote := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
	authority, _ := newISOTestSSHKey(t)
	host, _ := newISOTestSSHKey(t)
	certificate := &ssh.Certificate{
		Key:             host.PublicKey(),
		CertType:        ssh.HostCert,
		ValidPrincipals: []string{"upload.example.test"},
		ValidBefore:     ssh.CertTimeInfinity,
	}
	if err := certificate.SignCert(rand.Reader, authority); err != nil {
		t.Fatal(err)
	}
	caEntry := "@cert-authority " + knownhosts.Line([]string{address}, authority.PublicKey())
	unsupportedEntry := isoTestSSHUnsupportedHostEntry(t)
	for _, tc := range []struct{ name, knownHosts string }{
		{"authority only", caEntry},
		{"unsupported entry before authority", unsupportedEntry + "\n" + caEntry},
		{"unsupported entry after authority", caEntry + "\n" + unsupportedEntry},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.knownHosts)
			knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
			if err := os.WriteFile(knownHostsPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			verify, err := knownhosts.New(knownHostsPath)
			if err != nil {
				t.Fatal(err)
			}
			algorithms := seedGenerationSSHHostKeyAlgorithms(data, address, remote, verify)
			if len(algorithms) != 0 {
				t.Fatalf("certificate authorities must preserve default algorithms, got %v", algorithms)
			}
			if err := verify(address, remote, certificate); err != nil {
				t.Fatalf("trusted host certificate was rejected: %v", err)
			}
			if err := verify(address, remote, host.PublicKey()); err == nil {
				t.Fatal("certificate authority accepted an unpinned plain host key")
			}
		})
	}
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
