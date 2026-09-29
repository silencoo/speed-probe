package vendors

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"syscall"
)

// Only return stable categories: raw network errors can contain private URLs,
// server addresses or authentication material from an outbound implementation.
func NetworkErrorCode(err error) string {
	var dns *net.DNSError
	var network net.Error
	var cert *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var invalidCert x509.CertificateInvalidError
	var hostname x509.HostnameError
	var record tls.RecordHeaderError
	switch {
	case errors.Is(err, ErrResponseTooLarge):
		return "response_too_large"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.As(err, &dns):
		return "dns_error"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		return "timeout"
	case errors.As(err, &cert), errors.As(err, &unknownCA), errors.As(err, &invalidCert), errors.As(err, &hostname), errors.As(err, &record):
		return "tls_error"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection_closed"
	default:
		return "network_error"
	}
}
