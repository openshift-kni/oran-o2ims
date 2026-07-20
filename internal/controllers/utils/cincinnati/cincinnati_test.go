/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cincinnati

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	fakeclient "github.com/openshift-kni/oran-o2ims/test/fakeclient"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var testUpstream, _ = url.Parse("https://test.example.com/graph")
var testLogger = slog.New(slog.DiscardHandler)

func TestCincinnati(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cincinnati Suite")
}

var _ = Describe("findBestIntermediate", func() {
	// Graph layout (indices):
	// 0: 4.20.0, 1: 4.21.1, 2: 4.21.5, 3: 4.22.0, 4: 4.21.3 (no edge to target)
	testGraph := &graph{
		Nodes: []node{
			{Version: "4.20.0"},
			{Version: "4.21.1"},
			{Version: "4.21.5"},
			{Version: "4.22.0"},
			{Version: "4.21.3"},
		},
		Edges: [][]int{
			{0, 1}, // 4.20.0 -> 4.21.1
			{0, 2}, // 4.20.0 -> 4.21.5
			{0, 4}, // 4.20.0 -> 4.21.3
			{1, 3}, // 4.21.1 -> 4.22.0
			{2, 3}, // 4.21.5 -> 4.22.0
			// 4.21.3 has no edge to 4.22.0
		},
	}

	It("should select the latest patch with edges from current and to target", func() {
		selected, err := findBestIntermediateVersion(testGraph, testUpstream, "eus-4.22", "4.20.0", "4.22.0")
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(Equal("4.21.5"))
	})

	It("should error when current version is missing", func() {
		_, err := findBestIntermediateVersion(testGraph, testUpstream, "eus-4.22", "4.20.99", "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("current version 4.20.99 not found in update graph"))
	})

	It("should error when target version is missing", func() {
		_, err := findBestIntermediateVersion(testGraph, testUpstream, "eus-4.22", "4.20.0", "4.22.99")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("target version 4.22.99 not found in update graph"))
	})

	It("should error when no valid intermediate path exists", func() {
		g := &graph{
			Nodes: []node{
				{Version: "4.20.0"},
				{Version: "4.21.0"},
				{Version: "4.22.0"},
			},
			Edges: [][]int{
				{0, 1},
			},
		}
		_, err := findBestIntermediateVersion(g, testUpstream, "eus-4.22", "4.20.0", "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unable to select intermediate version"))
		Expect(err.Error()).To(ContainSubstring("no valid upgrade path found"))
	})

	It("should error for nil graph", func() {
		_, err := findBestIntermediateVersion(nil, testUpstream, "eus-4.22", "4.20.0", "4.22.0")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("getGraph", func() {
	It("should fetch and parse a graph response", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Query().Get("channel")).To(Equal("eus-4.22"))
			Expect(r.URL.Query().Get("arch")).To(Equal("amd64"))
			_ = json.NewEncoder(w).Encode(graph{
				Nodes: []node{{Version: "4.20.0"}, {Version: "4.21.0"}, {Version: "4.22.0"}},
				Edges: [][]int{{0, 1}, {1, 2}},
			})
		}))
		defer srv.Close()

		srvURL, _ := url.Parse(srv.URL)
		g, err := getGraph(context.Background(), testLogger, srv.Client(), srvURL, "eus-4.22", "amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(g.Nodes).To(HaveLen(3))
		Expect(g.Edges).To(HaveLen(2))
	})

	It("should return error on non-OK status", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer srv.Close()

		srvURL, _ := url.Parse(srv.URL)
		_, err := getGraph(context.Background(), testLogger, srv.Client(), srvURL, "stable-4.22", "amd64")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeFalse())
		Expect(err.Error()).To(ContainSubstring("status: 502"))
	})

	It("should return InputError on 404", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		srvURL, _ := url.Parse(srv.URL)
		_, err := getGraph(context.Background(), testLogger, srv.Client(), srvURL, "eus-4.22", "amd64")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("status: 404"))
	})

	It("should return InputError on DNS not found", func() {
		srvURL, _ := url.Parse("http://does-not-exist.invalid")
		_, err := getGraph(context.Background(), testLogger, http.DefaultClient, srvURL, "eus-4.22", "amd64")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("no such host"))
	})
})

var _ = Describe("SelectIntermediateVersion", func() {
	It("should select the latest intermediate version from a graph", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(graph{
				Nodes: []node{
					{Version: "4.20.0"},
					{Version: "4.21.1"},
					{Version: "4.21.5"},
					{Version: "4.22.0"},
				},
				Edges: [][]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}},
			})
		}))
		defer srv.Close()

		k8sClient := fake.NewClientBuilder().WithScheme(fakeclient.Scheme).Build()
		srvURL, _ := url.Parse(srv.URL)
		selected, err := SelectIntermediateVersion(
			context.Background(), k8sClient, testLogger,
			srvURL, "eus-4.22", "amd64", "4.20.0", "4.22.0")
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(Equal("4.21.5"))
	})

	It("should return InputError when no valid path exists", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(graph{
				Nodes: []node{
					{Version: "4.20.0"},
					{Version: "4.21.0"},
					{Version: "4.22.0"},
				},
				Edges: [][]int{{0, 1}},
			})
		}))
		defer srv.Close()

		k8sClient := fake.NewClientBuilder().WithScheme(fakeclient.Scheme).Build()
		srvURL, _ := url.Parse(srv.URL)
		_, err := SelectIntermediateVersion(
			context.Background(), k8sClient, testLogger,
			srvURL, "eus-4.22", "amd64", "4.20.0", "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("no valid upgrade path found"))
	})

	It("should return error when server is unavailable", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer srv.Close()

		k8sClient := fake.NewClientBuilder().WithScheme(fakeclient.Scheme).Build()
		srvURL, _ := url.Parse(srv.URL)
		_, err := SelectIntermediateVersion(
			context.Background(), k8sClient, testLogger,
			srvURL, "eus-4.22", "amd64", "4.20.0", "4.22.0")
		Expect(err).To(HaveOccurred())
		Expect(typederrors.IsInputError(err)).To(BeFalse())
		Expect(err.Error()).To(ContainSubstring("unable to retrieve update graph"))
	})

	It("should work when trusted-ca-bundle ConfigMap is not found", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(graph{
				Nodes: []node{{Version: "4.20.0"}, {Version: "4.21.0"}, {Version: "4.22.0"}},
				Edges: [][]int{{0, 1}, {1, 2}},
			})
		}))
		defer srv.Close()

		k8sClient := fake.NewClientBuilder().WithScheme(fakeclient.Scheme).Build()
		srvURL, _ := url.Parse(srv.URL)
		selected, err := SelectIntermediateVersion(
			context.Background(), k8sClient, testLogger,
			srvURL, "eus-4.22", "amd64", "4.20.0", "4.22.0")
		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(Equal("4.21.0"))
	})

})
