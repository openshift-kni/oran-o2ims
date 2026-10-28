/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const seedGenerationSSHPreflightTimeout = 15 * time.Second

var runSeedGenerationSSHCommand = runSeedGenerationSSHCommandOnServer

func validateSeedGenerationSSHUploadAccess(
	ctx context.Context,
	host string,
	uploadSecretData map[string][]byte,
	artifact *seedGenerationISOArtifact,
	prUID string,
) error {
	if artifact == nil || !seedGenerationRunIDPattern.MatchString(prUID) {
		return fmt.Errorf("valid run identity and upload paths are required")
	}
	// Probe the shared parent without reserving the final per-run directory.
	// New serving directories must be traversable by the HTTP server; the
	// temporary probe itself remains private and is removed on exit.
	parent := path.Dir(artifact.RemoteDirectory)
	probe := path.Join(parent, ".oran-seedgen-preflight-"+prUID+"-"+uuid.NewString())
	command := fmt.Sprintf("set -eu; umask 022; mkdir -p -- '%s'; (umask 077; mkdir -- '%s'; trap 'rm -f -- \"%s/access\"; rmdir -- \"%s\"' EXIT; : > '%s/access')",
		parent, probe, probe, probe, probe)
	if _, err := runSeedGenerationSSHCommand(ctx, host, uploadSecretData, command); err != nil {
		return fmt.Errorf("upload target SSH authentication, host key, or path validation failed: %w", err)
	}
	return nil
}

func runSeedGenerationSSHCommandOnServer(ctx context.Context, host string, data map[string][]byte, command string) ([]byte, error) {
	return runSeedGenerationSSHCommandAtAddress(ctx, net.JoinHostPort(host, "22"), data, command)
}

func runSeedGenerationSSHCommandAtAddress(ctx context.Context, address string, uploadSecretData map[string][]byte, command string) ([]byte, error) {
	username := string(uploadSecretData["username"])
	keyBytes := uploadSecretData["privateKey"]
	knownHostsBytes := uploadSecretData["knownHosts"]
	if address == "" || username == "" || len(keyBytes) == 0 || len(knownHostsBytes) == 0 {
		return nil, fmt.Errorf("upload credentials for SSH are incomplete")
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("upload private key is invalid or encrypted")
	}
	knownHostsFile, err := os.CreateTemp(os.Getenv("SEED_GENERATION_SSH_TMP_DIR"), "seedgen-known-hosts-")
	if err != nil {
		return nil, fmt.Errorf("failed to prepare SSH known-host verification: %w", err)
	}
	knownHostsPath := knownHostsFile.Name()
	defer os.Remove(knownHostsPath)
	if err := knownHostsFile.Chmod(0o600); err != nil {
		knownHostsFile.Close()
		return nil, fmt.Errorf("failed to prepare SSH known-host verification: %w", err)
	}
	if _, err := knownHostsFile.Write(knownHostsBytes); err != nil {
		knownHostsFile.Close()
		return nil, fmt.Errorf("failed to prepare SSH known-host verification: %w", err)
	}
	if err := knownHostsFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to prepare SSH known-host verification: %w", err)
	}
	knownHostCallback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("known_hosts data for SSH is invalid")
	}

	checkContext, cancel := context.WithTimeout(ctx, seedGenerationSSHPreflightTimeout)
	defer cancel()
	dialer := &net.Dialer{Timeout: seedGenerationSSHPreflightTimeout}
	connection, err := dialer.DialContext(checkContext, "tcp", address)
	if err != nil {
		if isSeedGenerationTransientError(err) {
			return nil, markSeedGenerationTransient(fmt.Errorf("upload server for SSH is not reachable: %w", err))
		}
		return nil, fmt.Errorf("upload server for SSH is not reachable")
	}
	deadline := time.Now().Add(seedGenerationSSHPreflightTimeout)
	if contextDeadline, ok := checkContext.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		connection.Close()
		return nil, fmt.Errorf("failed to set SSH upload deadline")
	}
	hostKeyAlgorithms, err := seedGenerationSSHHostKeyAlgorithms(
		knownHostsBytes, address, connection.RemoteAddr(), knownHostCallback)
	if err != nil {
		connection.Close()
		return nil, fmt.Errorf("known_hosts data for SSH is invalid")
	}
	clientConfig := &ssh.ClientConfig{
		User: username, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: knownHostCallback, HostKeyAlgorithms: hostKeyAlgorithms,
		Timeout: seedGenerationSSHPreflightTimeout,
	}
	clientConnection, channels, requests, err := ssh.NewClientConn(connection, address, clientConfig)
	if err != nil {
		connection.Close()
		if isSeedGenerationTransientError(err) {
			return nil, markSeedGenerationTransient(fmt.Errorf("upload server check via SSH could not complete: %w", err))
		}
		var revoked *knownhosts.RevokedError
		if errors.As(err, &revoked) {
			return nil, fmt.Errorf("upload server SSH host key is revoked")
		}
		var keyError *knownhosts.KeyError
		if errors.As(err, &keyError) {
			if len(keyError.Want) == 0 {
				return nil, fmt.Errorf("upload server SSH host key is not pinned in known_hosts")
			}
			return nil, fmt.Errorf("upload server SSH host key does not match known_hosts")
		}
		return nil, fmt.Errorf("upload SSH handshake or authentication failed")
	}
	sshClient := ssh.NewClient(clientConnection, channels, requests)
	defer sshClient.Close()
	session, err := sshClient.NewSession()
	if err != nil {
		if isSeedGenerationTransientError(err) {
			return nil, markSeedGenerationTransient(fmt.Errorf("upload server check via SSH could not complete: %w", err))
		}
		return nil, fmt.Errorf("upload server did not accept an SSH session")
	}
	defer session.Close()
	output := &seedGenerationSSHOutput{}
	session.Stdout = output
	session.Stderr = io.Discard
	if err := session.Run(command); err != nil {
		if isSeedGenerationTransientError(err) {
			return nil, markSeedGenerationTransient(fmt.Errorf("upload filesystem operation via SSH could not complete: %w", err))
		}
		return nil, fmt.Errorf("upload server rejected the SSH filesystem operation")
	}
	return output.Bytes(), nil
}

// Prefer host-key algorithms that correspond to keys pinned for this server.
// Unlike OpenSSH, the Go client does not automatically adjust its preference
// based on known_hosts, so a server with several keys can otherwise present
// an unpinned key even when the pinned one is available.
func seedGenerationSSHHostKeyAlgorithms(
	data []byte, address string, remote net.Addr, verify ssh.HostKeyCallback,
) ([]string, error) {
	var algorithms []string
	seen := make(map[string]bool)
	add := func(algorithm string) {
		if !seen[algorithm] {
			algorithms = append(algorithms, algorithm)
			seen[algorithm] = true
		}
	}
	for len(data) > 0 {
		marker, _, key, _, rest, err := ssh.ParseKnownHosts(data)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse SSH known_hosts entry: %w", err)
		}
		data = rest
		// Reuse knownhosts matching for hashed names, wildcard patterns, and revocations.
		if marker != "" || verify(address, remote, key) != nil {
			continue
		}
		if key.Type() == ssh.KeyAlgoRSA {
			add(ssh.KeyAlgoRSASHA512)
			add(ssh.KeyAlgoRSASHA256)
			add(ssh.KeyAlgoRSA)
		} else {
			add(key.Type())
		}
	}
	// An empty list preserves Go's defaults for certificate-authority-only
	// known_hosts files; the callback still verifies the presented certificate.
	return algorithms, nil
}

type seedGenerationSSHOutput struct{ buffer bytes.Buffer }

func (output *seedGenerationSSHOutput) Bytes() []byte { return output.buffer.Bytes() }
func (output *seedGenerationSSHOutput) Len() int      { return output.buffer.Len() }

func (output *seedGenerationSSHOutput) Write(data []byte) (int, error) {
	if remaining := (4 << 10) - output.Len(); remaining > 0 {
		_, _ = output.buffer.Write(data[:min(len(data), remaining)])
	}
	return len(data), nil
}
