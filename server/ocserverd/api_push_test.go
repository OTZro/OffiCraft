package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SherClockHolmes/webpush-go"
)

type recordingPushClient struct {
	received chan<- string
}

func (c recordingPushClient) Do(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Authorization") == "" || r.Header.Get("Content-Encoding") != "aes128gcm" {
		return nil, io.ErrUnexpectedEOF
	}
	if c.received != nil {
		c.received <- r.URL.String()
	}
	status := http.StatusCreated
	if strings.Contains(r.URL.Host, "expired") {
		status = http.StatusGone
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
}

var _ webpush.HTTPClient = recordingPushClient{}

func testPushSubscription(t *testing.T, endpoint string) PushSubscription {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return PushSubscription{
		Endpoint: endpoint,
		P256dh:   base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(auth),
	}
}

func TestPushPublicKeyPersistsAndIsBrowserUsable(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t)}
	first, err := s.pushPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.pushPublicKey()
	if err != nil || first != second {
		t.Fatalf("VAPID public key must be stable: %q %q %v", first, second, err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil || len(raw) != 65 || raw[0] != 4 {
		t.Fatalf("public key must be an uncompressed P-256 browser key: %x %v", raw, err)
	}
}

func TestWebPushDeliveryAndExpiredSubscriptionPruning(t *testing.T) {
	d := newTestDAL(t)
	urls := make(chan string, 2)
	s := &apiServer{dal: d, pushHTTPClient: recordingPushClient{received: urls}}
	if err := d.PutPushSubscription(testPushSubscription(t, "https://push.example.test/live")); err != nil {
		t.Fatal(err)
	}
	s.enqueueWebPush(webPushPayload{Kind: "chat", ChatID: "c-1", Title: "new", Body: "message"})
	select {
	case <-urls:
	case <-time.After(2 * time.Second):
		t.Fatal("push gateway did not receive the notification")
	}

	expiredEndpoint := "https://expired.example.test/push"
	if err := d.PutPushSubscription(testPushSubscription(t, expiredEndpoint)); err != nil {
		t.Fatal(err)
	}
	s.enqueueWebPush(webPushPayload{Kind: "reply_card", ReplyCardID: "rc-1", Title: "ask", Body: "decision", NeedsDecision: true})
	deadline := time.After(2 * time.Second)
	for {
		subs, err := d.ListPushSubscriptions()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, sub := range subs {
			if sub.Endpoint == expiredEndpoint {
				found = true
			}
		}
		if !found {
			break
		}
		select {
		case <-deadline:
			t.Fatal("410 subscription was not pruned")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestValidatePushEndpointRejectsNonPublicTargets(t *testing.T) {
	for _, endpoint := range []string{
		"http://push.example.test", "https://localhost/push", "https://127.0.0.1/push",
		"https://[::1]/push", "https://169.254.169.254/latest/meta-data", "https://100.64.0.1/push",
	} {
		if err := validatePushEndpoint(endpoint); err == nil {
			t.Errorf("%q must be rejected", endpoint)
		}
	}
	if err := validatePushEndpoint("https://push.example.test/subscription"); err != nil {
		t.Fatalf("public HTTPS endpoint must be accepted: %v", err)
	}
}
