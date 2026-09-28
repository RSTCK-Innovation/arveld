package monitor

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Validate normalizes a display name and validates exactly one protocol.
// Assignment existence and ID uniqueness belong to the Store.
func Validate(value Monitor) (Monitor, error) {
	value.Name = strings.TrimSpace(value.Name)
	length := utf8.RuneCountInString(value.Name)
	if !utf8.ValidString(value.Name) || length < 2 || length > 80 || strings.ContainsRune(value.Name, '\x00') {
		return Monitor{}, errors.New("monitor name must contain 2 to 80 characters without NUL")
	}
	if err := validateProtocolOptions(value); err != nil {
		return Monitor{}, err
	}
	var err error
	switch value.Protocol {
	case "http":
		err = ValidateHTTPSettings(value.Endpoint, value.Method, value.IntervalSeconds, value.TimeoutSeconds)
	case "tcp":
		err = ValidateTCPSettings(value.Endpoint, value.IntervalSeconds, value.TimeoutSeconds)
	case "icmp":
		err = ValidateICMPSettings(value.Endpoint, value.PingCount, value.IntervalSeconds, value.TimeoutSeconds)
	case "dns":
		err = ValidateDNSSettings(value.Endpoint, value.DNSServer, value.RecordType, value.Transport, value.IntervalSeconds, value.TimeoutSeconds)
	default:
		err = errors.New("unsupported Monitor protocol")
	}
	if err != nil {
		return Monitor{}, err
	}
	return value, nil
}

func validateProtocolOptions(value Monitor) error {
	if value.Protocol != "dns" && (value.DNSServer != "" || value.RecordType != "" || value.Transport != "") {
		return errors.New("DNS settings require a DNS Monitor")
	}
	if value.Protocol != "icmp" && value.PingCount != 0 {
		return errors.New("ping count requires an ICMP Monitor")
	}
	if value.Protocol != "http" && (value.Method != "" || value.Body != "" || len(value.Headers) != 0 || len(value.Validations) != 0) {
		return errors.New("method requires an HTTP Monitor")
	}
	if value.SkipTLSVerify && value.Protocol != "http" {
		return errors.New("TLS verification setting requires an HTTP Monitor")
	}
	if err := ValidateHTTPOptions(value.HTTPOptions); err != nil {
		return err
	}
	return nil
}

// ValidateHTTPSettings checks the probe settings shared by product writes and compilation.
func ValidateHTTPSettings(endpointURL, method string, intervalSeconds, timeoutSeconds int) error {
	endpoint, err := url.Parse(endpointURL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" || endpoint.User != nil {
		return errors.New("endpoint must be an absolute HTTP or HTTPS URL without credentials")
	}
	if port := endpoint.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return errors.New("endpoint port must be between 1 and 65535")
		}
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
	default:
		return errors.New("unsupported HTTP method")
	}
	return validateTiming(intervalSeconds, timeoutSeconds)
}

// ValidateTCPSettings validates connection settings shared by product writes and compilation.
func ValidateTCPSettings(endpoint string, intervalSeconds, timeoutSeconds int) error {
	if err := validateHostPort(endpoint); err != nil {
		return err
	}
	return validateTiming(intervalSeconds, timeoutSeconds)
}

// ValidateICMPSettings validates a host and a bounded sequence of one-second-spaced pings.
func ValidateICMPSettings(endpoint string, pingCount, intervalSeconds, timeoutSeconds int) error {
	if !validHost(endpoint) {
		return errors.New("endpoint must be a hostname or IP address without a port")
	}
	if pingCount < 1 || pingCount > 10 || pingCount > timeoutSeconds {
		return errors.New("ping count must be between 1 and 10 and must not exceed the timeout in seconds")
	}
	return validateTiming(intervalSeconds, timeoutSeconds)
}

// ValidateDNSSettings validates an explicit resolver and one supported DNS query.
func ValidateDNSSettings(endpoint, server, recordType, transport string, intervalSeconds, timeoutSeconds int) error {
	if !validDNSName(endpoint) {
		return errors.New("endpoint must be a DNS name without a scheme or port")
	}
	if err := validateHostPort(server); err != nil {
		return err
	}
	switch recordType {
	case "A", "AAAA", "CNAME", "MX", "TXT", "NS":
	default:
		return errors.New("unsupported DNS record type")
	}
	if transport != "udp" && transport != "tcp" && transport != "tcp-tls" {
		return errors.New("DNS transport must be udp, tcp or tcp-tls")
	}
	return validateTiming(intervalSeconds, timeoutSeconds)
}

func validateHostPort(endpoint string) error {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || !validHost(host) {
		return errors.New("endpoint must contain a valid host and port without a scheme, credentials or environment references")
	}
	if strings.ContainsAny(port, "+-") {
		return errors.New("endpoint port must contain only digits")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("endpoint port must be between 1 and 65535")
	}
	return nil
}

func validHost(host string) bool {
	if strings.ContainsAny(host, "$\x00\r\n\t ") {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	return validDNSName(host)
}

func validDNSName(name string) bool {
	name = strings.TrimSuffix(name, ".")
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for label := range strings.SplitSeq(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			switch {
			case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z',
				character >= '0' && character <= '9', character == '-', character == '_':
			default:
				return false
			}
		}
	}
	return true
}

func validateTiming(intervalSeconds, timeoutSeconds int) error {
	if intervalSeconds < 10 || intervalSeconds > 3600 {
		return errors.New("interval must be between 10 and 3600 seconds")
	}
	if timeoutSeconds < 1 || timeoutSeconds > 60 || timeoutSeconds > intervalSeconds {
		return errors.New("timeout must be between 1 and 60 seconds and must not exceed the interval")
	}
	return nil
}
