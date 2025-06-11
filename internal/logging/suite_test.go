/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package logging

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

func TestLogging(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Logging")
}

var _ = BeforeSuite(func() {
	log.SetOutput(GinkgoWriter)
})

// Parse parses a set of log lines into a slice of maps that is easier to analyze.
func Parse(buffer io.Reader) []map[string]any {
	scanner := bufio.NewScanner(buffer)
	var result []map[string]any
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		ExpectWithOffset(1, scanner.Err()).ToNot(HaveOccurred())
		var message map[string]any
		err := json.Unmarshal(line, &message)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
		result = append(result, message)
	}
	return result
}

// Find finds the subset of messages that contain the given `msg`.
func Find(messages []map[string]any, msg string) []map[string]any {
	var result []map[string]any
	for _, message := range messages {
		value, ok := message["msg"]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if text != msg {
			continue
		}
		result = append(result, message)
	}
	return result
}
