package monitor

import (
	"errors"
	"net/textproto"
	"regexp"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// HTTPOptions contains request settings and response assertions supported by the Agent.
type HTTPOptions struct {
	Body        string            `json:"body,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Validations []Validation      `json:"validations,omitempty"`
}

// Validation describes one assertion. Equals distinguishes existence (nil) from
// equality. Empty equality is rejected because the Agent treats it as existence.
// Size is measured in bytes.
type Validation struct {
	Type   string  `json:"type"`
	Value  string  `json:"value,omitempty"`
	Path   string  `json:"path,omitempty"`
	Equals *string `json:"equals,omitempty"`
	Size   *int64  `json:"size,omitempty"`
}

// ValidateHTTPOptions rejects malformed options before persistence or compilation.
func ValidateHTTPOptions(options HTTPOptions) error {
	if len(options.Body) > 64*1024 || len(options.Headers) > 32 || len(options.Validations) > 32 {
		return errors.New("HTTP options exceed the body, header or validation limit")
	}
	names := make(map[string]bool, len(options.Headers))
	total := 0
	for name, value := range options.Headers {
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) || names[canonical] {
			return errors.New("HTTP headers must have valid, unique names and values")
		}
		switch canonical {
		case "Content-Length", "Transfer-Encoding", "Connection", "Proxy-Connection", "Upgrade", "Trailer", "Te", "Keep-Alive":
			return errors.New("HTTP framing and connection headers are managed by the Agent")
		}
		names[canonical] = true
		total += len(name) + len(value)
	}
	if total > 16*1024 {
		return errors.New("HTTP headers exceed 16 KiB")
	}
	for _, rule := range options.Validations {
		if err := rule.validate(); err != nil {
			return err
		}
	}
	return nil
}

// validate checks one assertion without evaluating a response.
func (rule Validation) validate() error {
	if len(rule.Value) > 4096 || len(rule.Path) > 1024 || rule.Equals != nil && len(*rule.Equals) > 4096 {
		return errors.New("HTTP validation exceeds its size limit")
	}
	switch rule.Type {
	case "contains", "not_contains", "regex":
		if rule.Value == "" {
			return errors.New("text validation requires a nonempty value")
		}
		if rule.Path != "" || rule.Equals != nil || rule.Size != nil {
			return errors.New("text validation has incompatible settings")
		}
		if rule.Type == "regex" {
			if _, err := regexp.Compile(rule.Value); err != nil {
				return errors.New("invalid HTTP validation regular expression")
			}
		}
	case "json_path":
		if rule.Equals != nil && *rule.Equals == "" {
			return errors.New("the Agent cannot assert JSON equality with an empty string")
		}
		if strings.TrimSpace(rule.Path) == "" || rule.Value != "" || rule.Size != nil {
			return errors.New("JSON validation requires a GJSON path")
		}
	case "min_size", "max_size":
		return rule.validateSize()
	default:
		return errors.New("unsupported HTTP validation type")
	}
	return nil
}

func (rule Validation) validateSize() error {
	if rule.Size == nil || *rule.Size < 0 || *rule.Size > 4*1024*1024 || rule.Value != "" || rule.Path != "" || rule.Equals != nil {
		return errors.New("body size validation must be between 0 and 4 MiB")
	}
	return nil
}
