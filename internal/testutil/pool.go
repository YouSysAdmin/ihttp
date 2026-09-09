package testutil

import (
	"crypto/x509"

	"github.com/yousysadmin/ihttp/internal/core/certgen"
)

func rootPool(ca *certgen.Authority) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(ca.CA())

	return pool
}
