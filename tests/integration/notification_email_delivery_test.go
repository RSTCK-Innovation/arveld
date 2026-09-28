package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/components"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestNativeEmailNotificationLifecycle(t *testing.T) {
	if os.Getenv("ARVELD_TEST_ALERTMANAGER_BINARY") == "" {
		t.Skip("set the pinned Alertmanager executable to run native SMTP delivery")
	}
	for _, mode := range []string{"none", "starttls", "tls"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			resource := "Homelab"
			if mode == "tls" {
				resource = `Homelab <img src=x onerror=alert(1)>`
			}
			// Borrow httptest's localhost certificate as a test-only trust anchor.
			certificates := httptest.NewTLSServer(http.NotFoundHandler())
			certificate := certificates.TLS.Certificates[0]
			certificates.Close()
			tlsConfig := &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
			address, received := startSMTPReceiver(t, mode, tlsConfig)
			db := testutil.OpenDatabase(t, filepath.Join(directory, "arveld.db"))
			store := notification.NewStore(db)
			config := map[string]string{"smarthost": address, "from": "arveld@example.test", "to": "operations@example.test", "tls_mode": mode}
			if mode != "none" {
				config["auth_username"], config["auth_password"] = "arveld", " test-only password "
			}
			encoded, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Create(t.Context(), notification.Channel{
				ID: "email", Name: "Email", Type: "email", Config: encoded,
				Delivery: &notification.DeliverySettings{GroupBy: "resource", GroupWaitSeconds: 1, GroupIntervalSeconds: 1, RepeatIntervalSeconds: 60},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), `
 INSERT INTO agents (instance_uid) VALUES (zeroblob(16));
 INSERT INTO alert_rules (id,agent_instance_uid,condition,threshold,for_seconds,severity) VALUES
 ('cpu',zeroblob(16),'cpu',80,1,'warning'),('memory',zeroblob(16),'memory',80,1,'warning');
 INSERT INTO alert_rule_notifications (rule_id,notification_id) VALUES ('cpu','email'),('memory','email');
 `); err != nil {
				t.Fatal(err)
			}
			content, err := store.AlertmanagerConfig(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if mode != "none" {
				caPath := filepath.Join(directory, "smtp-ca.pem")
				if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0o600); err != nil {
					t.Fatal(err)
				}
				// Only the isolated engine trusts this CA; product configuration keeps system roots.
				content = append(content, []byte("\nglobal:\n  smtp_tls_config:\n    ca_file: "+strconv.Quote(caPath)+"\n")...)
			}
			if _, err := components.WriteAlertmanagerConfig(directory, content); err != nil {
				t.Fatal(err)
			}
			engine := startNotificationAlertmanager(t, directory)
			start := time.Now().UTC()
			emit := func(end time.Time) {
				t.Helper()
				var alerts []map[string]any
				for _, id := range []string{"cpu", "memory"} {
					alerts = append(alerts, map[string]any{"labels": map[string]string{"alertname": "ArveldAgent" + id, "arveld_rule_id": id, "arveld_rule_revision": "revision", "arveld_agent_id": "00000000-0000-0000-0000-000000000000", "severity": "warning"}, "annotations": map[string]string{"resource": resource, "summary": "High " + id + " usage", "description": "Usage is above 80%."}, "startsAt": start, "endsAt": end.UTC()})
				}
				body, err := json.Marshal(alerts)
				if err != nil {
					t.Fatal(err)
				}
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, engine+"/api/v2/alerts", bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				response, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				if err := response.Body.Close(); err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != http.StatusOK {
					t.Fatalf("native alert ingestion = %d", response.StatusCode)
				}
			}
			await := func(status string) {
				t.Helper()
				select {
				case value := <-received:
					if value.from != "MAIL FROM:<arveld@example.test>" || value.to != "RCPT TO:<operations@example.test>" {
						t.Fatalf("wrong email envelope: %q %q", value.from, value.to)
					}
					message, err := mail.ReadMessage(bytes.NewReader(value.data))
					if err != nil {
						t.Fatal(err)
					}
					subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
					if err != nil || subject != "Arveld · "+status+" · "+resource {
						t.Fatalf("email subject = %q, error = %v", subject, err)
					}
					_, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
					if err != nil {
						t.Fatal(err)
					}
					parts := multipart.NewReader(message.Body, params["boundary"])
					var text []byte
					for {
						part, err := parts.NextPart()
						if errors.Is(err, io.EOF) {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
						body, err := io.ReadAll(part)
						if err != nil {
							t.Fatal(err)
						}
						if strings.HasPrefix(part.Header.Get("Content-Type"), "text/html") {
							if bytes.Contains(body, []byte("<img src=x")) {
								t.Fatal("resource name became email markup")
							}
							if output := os.Getenv("ARVELD_TEST_NOTIFICATION_PREVIEW_DIR"); output != "" && mode == "none" {
								name := "active.html"
								if status == "Recovered" {
									name = "recovered.html"
								}
								if err := os.WriteFile(filepath.Join(output, name), body, 0o600); err != nil { //nolint:gosec // Opt-in export directory supplied by the local test runner; filenames are fixed above.
									t.Fatal(err)
								}
							}
						}
						text = append(text, body...)
					}
					if !bytes.Contains(text, []byte("High cpu usage")) || !bytes.Contains(text, []byte("High memory usage")) {
						t.Fatal("email omitted the grouped alert details")
					}
					explanation := "Usage is above 80%."
					if status == "Recovered" {
						explanation = "This condition is no longer active."
					}
					if !bytes.Contains(text, []byte(explanation)) {
						t.Fatal("email omitted the explanation for its state")
					}
					for _, technical := range []string{"arveld_rule_id", "arveld_rule_revision", "Alertmanager", "ArveldAgent", "00000000-0000-0000-0000-000000000000"} {
						if bytes.Contains(text, []byte(technical)) {
							t.Fatalf("email exposes internal detail %q", technical)
						}
					}
				case <-time.After(12 * time.Second):
					t.Fatal("no native SMTP delivery")
				}
			}
			emit(time.Now().Add(time.Hour))
			await("2 alerts active")
			emit(time.Now())
			await("Recovered")
		})
	}
}

type smtpMessage struct {
	from, to string
	data     []byte
}

// This fixture implements only the SMTP operations used at the transport boundary.
func startSMTPReceiver(t *testing.T, mode string, config *tls.Config) (string, <-chan smtpMessage) {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if mode == "tls" {
		listener = tls.NewListener(listener, config)
	}
	received := make(chan smtpMessage, 8)
	var workers sync.WaitGroup
	workers.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					t.Error(err)
				}
				return
			}
			workers.Go(func() {
				defer func() { _ = conn.Close() }() //nolint:errcheck // Close the test-owned socket after the protocol session.
				if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
					t.Error(err)
					return
				}
				if err := serveSMTP(t.Context(), conn, mode, config, received); err != nil {
					t.Error(err)
				}
			})
		}
	})
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
		workers.Wait()
	})
	return listener.Addr().String(), received
}

func serveSMTP(ctx context.Context, conn net.Conn, mode string, config *tls.Config, received chan<- smtpMessage) error {
	protocol := textproto.NewConn(conn)
	if err := protocol.PrintfLine("220 smtp-fixture ESMTP"); err != nil {
		return fmt.Errorf("SMTP fixture I/O: %w", err)
	}
	secure, authenticated := mode == "tls", mode == "none"
	var message smtpMessage
	for {
		line, err := protocol.ReadLine()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("SMTP fixture I/O: %w", err)
		}
		reply := "250 OK"
		switch {
		case strings.HasPrefix(line, "EHLO "):
			reply = "250 smtp-fixture"
			if mode == "starttls" && !secure {
				reply = "250-smtp-fixture\r\n250 STARTTLS"
			} else if secure {
				reply = "250-smtp-fixture\r\n250 AUTH PLAIN"
			}
		case line == "STARTTLS":
			if mode != "starttls" || secure {
				return errors.New("unexpected STARTTLS command")
			}
			if err := protocol.PrintfLine("220 Ready for TLS"); err != nil {
				return fmt.Errorf("SMTP fixture I/O: %w", err)
			}
			secured := tls.Server(conn, config)
			if err := secured.HandshakeContext(ctx); err != nil {
				return fmt.Errorf("SMTP fixture I/O: %w", err)
			}
			protocol = textproto.NewConn(secured)
			secure = true
			continue
		case strings.HasPrefix(line, "AUTH PLAIN "):
			credential, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
			if err != nil || !secure || string(credential) != "\x00arveld\x00 test-only password " {
				return errors.New("incorrect SMTP authentication or missing TLS")
			}
			authenticated = true
			reply = "235 Authentication successful"
		case strings.HasPrefix(line, "MAIL FROM:"):
			if !authenticated {
				return errors.New("SMTP credentials were not used")
			}
			message.from = line
		case strings.HasPrefix(line, "RCPT TO:"):
			message.to = line
		case line == "DATA":
			if err := protocol.PrintfLine("354 End data with a dot"); err != nil {
				return fmt.Errorf("SMTP fixture I/O: %w", err)
			}
			message.data, err = protocol.ReadDotBytes()
			if err != nil {
				return fmt.Errorf("SMTP fixture I/O: %w", err)
			}
			select {
			case received <- message:
			default:
				return errors.New("unexpected SMTP notification flood")
			}
		case line == "QUIT":
			if err := protocol.PrintfLine("221 Bye"); err != nil {
				return fmt.Errorf("close SMTP session: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("unexpected SMTP operation: %s", strings.Split(line, " ")[0])
		}
		if err := protocol.PrintfLine("%s", reply); err != nil {
			return fmt.Errorf("SMTP fixture I/O: %w", err)
		}
	}
}
