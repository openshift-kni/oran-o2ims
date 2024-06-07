package server

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Flags", func() {
	It("Checks if the backend token and token file have been simultaneously provided", func() {
		err := testFlagSet.Set(backendTokenFlagName, "!@#QWEASDzzxc")
		Expect(err).ToNot(HaveOccurred())
		err = testFlagSet.Set(backendTokenFileFlagName, "/home/oran/test")
		Expect(err).ToNot(HaveOccurred())
		result, err := GetTokenFlag(testCtx, testFlagSet, testLogger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf(
				"backend token flag '--%s' and token file flag '--%s' have both been provided, "+
					"but they are incompatible",
				backendTokenFlagName, backendTokenFileFlagName)))
		Expect(result).To(BeEmpty())
	})

	It("Reads the backend token file if needed", func() {
		err := testFlagSet.Set(backendTokenFlagName, "")
		Expect(err).ToNot(HaveOccurred())
		err = testFlagSet.Set(backendTokenFileFlagName, "./flags.go")
		Expect(err).ToNot(HaveOccurred())
		result, err := GetTokenFlag(testCtx, testFlagSet, testLogger)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeEmpty())
		Expect(result).To(ContainSubstring("Copyright 2023 Red Hat Inc."))
	})

	It("Reads the backend token file if needed and returns error if the file doesn't exist", func() {
		err := testFlagSet.Set(backendTokenFlagName, "")
		Expect(err).ToNot(HaveOccurred())
		err = testFlagSet.Set(backendTokenFileFlagName, "./token")
		Expect(err).ToNot(HaveOccurred())
		result, err := GetTokenFlag(testCtx, testFlagSet, testLogger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("open ./token: no such file or directory"))
		Expect(result).To(BeEmpty())
	})

	It("Checks if no token is provided and returns explicit error", func() {
		err := testFlagSet.Set(backendTokenFlagName, "")
		Expect(err).ToNot(HaveOccurred())
		err = testFlagSet.Set(backendTokenFileFlagName, "")
		Expect(err).ToNot(HaveOccurred())
		result, err := GetTokenFlag(testCtx, testFlagSet, testLogger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf(
				"backend token '--%s' or token file '--%s' parameters must be provided",
				backendTokenFlagName,
				backendTokenFileFlagName)))
		Expect(result).To(BeEmpty())
	})

	It("Returns the right backend if no other errors occur", func() {
		err := testFlagSet.Set(backendTokenFlagName, "!@#qweASDzx")
		Expect(err).ToNot(HaveOccurred())
		err = testFlagSet.Set(backendTokenFileFlagName, "")
		Expect(err).ToNot(HaveOccurred())
		result, err := GetTokenFlag(testCtx, testFlagSet, testLogger)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("!@#qweASDzx"))
	})
})
