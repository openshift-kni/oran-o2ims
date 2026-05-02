/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
)

type stubDataSource struct {
	name string
	id   uuid.UUID
	gen  int
}

func (s *stubDataSource) Name() string { return s.name }

func (s *stubDataSource) GetID() uuid.UUID { return s.id }

func (s *stubDataSource) Init(dataSourceID uuid.UUID, generationID int, _ chan<- *async.AsyncChangeEvent) {
	s.id = dataSourceID
	s.gen = generationID
}

func (s *stubDataSource) SetGenerationID(value int) { s.gen = value }

func (s *stubDataSource) GetGenerationID() int { return s.gen }

func (s *stubDataSource) IncrGenerationID() int {
	s.gen++
	return s.gen
}

var _ = Describe("Collector findDataSource", func() {
	It("returns the data source whose Name matches", func() {
		a := &stubDataSource{name: "alpha"}
		b := &stubDataSource{name: "beta"}
		c := NewCollector(nil, nil, nil, []DataSource{a, b})
		Expect(c.findDataSource("beta")).To(Equal(b))
		Expect(c.findDataSource("missing")).To(BeNil())
	})
})
