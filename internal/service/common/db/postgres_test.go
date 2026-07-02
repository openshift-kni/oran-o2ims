/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package db

import (
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
)

var _ = Describe("buildPoolDSN", func() {
	var cfg PgConfig

	BeforeEach(func() {
		cfg = PgConfig{
			Host:     "db.example.svc.cluster.local",
			Port:     "5432",
			User:     "testuser",
			Password: "testpass",
			Database: "testdb",
		}
	})

	It("escapes special characters in password", func() {
		cfg.Password = "p@ss:word?with&special=chars/slash"
		dsn := buildPoolDSN(cfg, url.Values{"sslmode": {"require"}})

		parsed, err := url.Parse(dsn)
		Expect(err).ToNot(HaveOccurred())

		password, set := parsed.User.Password()
		Expect(set).To(BeTrue())
		Expect(password).To(Equal("p@ss:word?with&special=chars/slash"))
	})

	It("escapes special characters in username", func() {
		cfg.User = "user@domain"
		dsn := buildPoolDSN(cfg, url.Values{"sslmode": {"require"}})

		parsed, err := url.Parse(dsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(parsed.User.Username()).To(Equal("user@domain"))
	})

	It("uses postgres scheme", func() {
		dsn := buildPoolDSN(cfg, url.Values{"sslmode": {"require"}})

		parsed, err := url.Parse(dsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(parsed.Scheme).To(Equal("postgres"))
	})

	It("joins host and port", func() {
		dsn := buildPoolDSN(cfg, url.Values{"sslmode": {"require"}})

		parsed, err := url.Parse(dsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(parsed.Host).To(Equal("db.example.svc.cluster.local:5432"))
	})

	It("sets database as path", func() {
		dsn := buildPoolDSN(cfg, url.Values{"sslmode": {"require"}})

		parsed, err := url.Parse(dsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(parsed.Path).To(Equal("/testdb"))
	})

	It("passes sslmode=verify-full with sslrootcert", func() {
		params := url.Values{
			"sslmode":     {"verify-full"},
			"sslrootcert": {constants.DefaultServiceCAFile},
		}
		dsn := buildPoolDSN(cfg, params)

		parsed, err := url.Parse(dsn)
		Expect(err).ToNot(HaveOccurred())

		query := parsed.Query()
		Expect(query.Get("sslmode")).To(Equal("verify-full"))
		Expect(query.Get("sslrootcert")).To(Equal(constants.DefaultServiceCAFile))
	})

	It("passes sslmode=verify-full without sslrootcert", func() {
		params := url.Values{
			"sslmode": {"verify-full"},
		}
		dsn := buildPoolDSN(cfg, params)

		parsed, err := url.Parse(dsn)
		Expect(err).ToNot(HaveOccurred())

		query := parsed.Query()
		Expect(query.Get("sslmode")).To(Equal("verify-full"))
		Expect(query.Get("sslrootcert")).To(BeEmpty())
	})
})
