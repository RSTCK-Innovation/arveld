package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/mail"
	"strconv"
	"strings"
)

// emailConfig is the persisted SMTP contract. Addresses are literal, single
// mailboxes; templates and transport-specific native options are not user input.
type emailConfig struct {
	Smarthost    string `json:"smarthost"`
	From         string `json:"from"`
	To           string `json:"to"`
	TLSMode      string `json:"tls_mode"`
	AuthUsername string `json:"auth_username"`
	AuthPassword string `json:"auth_password"`
}

func normalizeEmailConfig(encoded json.RawMessage) (json.RawMessage, error) {
	config := emailConfig{TLSMode: "starttls"}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, ErrInvalidChannel
	}
	config.Smarthost = strings.TrimSpace(config.Smarthost)
	config.From, config.To = strings.TrimSpace(config.From), strings.TrimSpace(config.To)
	if !validMailbox(config.From) || !validMailbox(config.To) {
		return nil, ErrInvalidChannel
	}
	host, port, err := net.SplitHostPort(config.Smarthost)
	if err != nil || host == "" || len(host) > 253 || strings.ContainsAny(host, "/@?#\\%[]") || !printableASCII(host) {
		return nil, ErrInvalidChannel
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return nil, ErrInvalidChannel
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return nil, ErrInvalidChannel
	}
	config.Smarthost = net.JoinHostPort(host, strconv.Itoa(number))
	if err := validateEmailTransport(config); err != nil {
		return nil, err
	}
	result, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode email configuration: %w", err)
	}
	return result, nil
}

func validMailbox(value string) bool {
	if len(value) > 254 || !printableASCII(value) || strings.ContainsAny(value, "{}") {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value
}

// printableASCII excludes spaces, control characters and SMTPUTF8 addresses.
func printableASCII(value string) bool {
	for _, char := range value {
		if char <= 32 || char >= 127 {
			return false
		}
	}
	return value != ""
}

func validateEmailTransport(config emailConfig) error {
	if config.TLSMode != "starttls" && config.TLSMode != "tls" && config.TLSMode != "none" {
		return ErrInvalidChannel
	}
	if len(config.AuthUsername) > 254 || len(config.AuthPassword) > 1024 || strings.ContainsAny(config.AuthUsername+config.AuthPassword, "\x00\r\n") {
		return ErrInvalidChannel
	}
	if (config.AuthUsername == "") != (config.AuthPassword == "") {
		return ErrInvalidChannel
	}
	if config.TLSMode == "none" && config.AuthUsername != "" {
		return ErrInvalidChannel
	}
	return nil
}
