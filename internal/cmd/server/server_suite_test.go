package server

import (
	"context"
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"
)

// Logger used for tests:
var testLogger *slog.Logger
var testCtx context.Context
var testFlagSet *pflag.FlagSet

func TestServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Server Suite")
}

var _ = BeforeSuite(func() {
	testCtx = context.TODO()

	testFlagSet = pflag.NewFlagSet("commandName", pflag.ContinueOnError)
	AddTokenFlags(testFlagSet)

	options := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(GinkgoWriter, options)
	testLogger = slog.New(handler)
})
