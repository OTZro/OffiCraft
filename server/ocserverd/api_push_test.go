package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SherClockHolmes/webpush-go"
)

type recordingPushClient struct {
	received      chan<- string
	authorization chan<- string
	status        int
	statusFor     func(*http.Request) int
	err           error
}

func (c recordingPushClient) Do(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Authorization") == "" || r.Header.Get("Content-Encoding") != "aes128gcm" {
		return nil, io.ErrUnexpectedEOF
	}
	if c.received != nil {
		c.received <- r.URL.String()
	}
	if c.authorization != nil {
		c.authorization <- r.Header.Get("Authorization")
	}
	if c.err != nil {
		return nil, c.err
	}
	status := c.status
	if c.statusFor != nil {
		status = c.statusFor(r)
	}
	if status == 0 {
		status = http.StatusCreated
	}
	if status == http.StatusCreated && strings.Contains(r.URL.Host, "expired") {
		status = http.StatusGone
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
}

var _ webpush.HTTPClient = recordingPushClient{}

func vapidSubject(t *testing.T, authorization string) string {
	t.Helper()
	const prefix = "vapid t="
	if !strings.HasPrefix(authorization, prefix) {
		t.Fatalf("not a VAPID authorization header: %q", authorization)
	}
	token := strings.SplitN(strings.TrimPrefix(authorization, prefix), ",", 2)[0]
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed VAPID JWT: %q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Subject string `json:"sub"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	return claims.Subject
}

type lockedLogBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func waitForLog(t *testing.T, logs *lockedLogBuffer, want string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !strings.Contains(logs.String(), want) {
		select {
		case <-deadline:
			t.Fatalf("did not log %q; got %q", want, logs.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

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

func TestCreatePushSubscriptionPersistsBrowserSubscription(t *testing.T) {
	d := newTestDAL(t)
	s := &apiServer{dal: d}
	subscription := testPushSubscription(t, "https://push.example.test/browser-subscription")
	rec := httptest.NewRecorder()
	s.HandleCreatePushSubscriptionApiPushSubscriptionPost(rec, taskReq(t, http.MethodPost,
		"/api/push/subscription", map[string]any{
			"endpoint": subscription.Endpoint,
			"keys":     map[string]string{"p256dh": subscription.P256dh, "auth": subscription.Auth},
		}, "owner", "owner"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("subscription POST = %d: %s", rec.Code, rec.Body.String())
	}
	subs, err := d.ListPushSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Endpoint != subscription.Endpoint ||
		subs[0].P256dh != subscription.P256dh || subs[0].Auth != subscription.Auth {
		t.Fatalf("stored subscription = %+v, want endpoint and keys from POST", subs)
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

	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			expiredEndpoint := "https://push.example.test/expired-" + strconv.Itoa(status)
			if err := d.PutPushSubscription(testPushSubscription(t, expiredEndpoint)); err != nil {
				t.Fatal(err)
			}
			s.pushHTTPClient = recordingPushClient{statusFor: func(r *http.Request) int {
				if r.URL.String() == expiredEndpoint {
					return status
				}
				return http.StatusCreated
			}}
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
					liveFound := false
					for _, sub := range subs {
						if sub.Endpoint == "https://push.example.test/live" {
							liveFound = true
						}
					}
					if !liveFound {
						t.Fatal("an expired receipt must not prune another live subscription")
					}
					break
				}
				select {
				case <-deadline:
					t.Fatalf("%d subscription was not pruned", status)
				case <-time.After(10 * time.Millisecond):
				}
			}
		})
	}
}

func TestWebPushUsesPublicHTTPSVAPIDSubject(t *testing.T) {
	d := newTestDAL(t)
	authorization := make(chan string, 1)
	s := &apiServer{dal: d, pushHTTPClient: recordingPushClient{authorization: authorization}}
	if err := d.PutPushSubscription(testPushSubscription(t, "https://push.example.test/live")); err != nil {
		t.Fatal(err)
	}
	s.enqueueWebPush(webPushPayload{Kind: "chat", ChatID: "c-1", Title: "new", Body: "message"})
	select {
	case header := <-authorization:
		if got, want := vapidSubject(t, header), pushVAPIDSubscriber; got != want {
			t.Fatalf("VAPID subject = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("push gateway did not receive the notification")
	}
}

func TestWebPushLogsSafeGatewayStatuses(t *testing.T) {
	var logs lockedLogBuffer
	oldOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldOutput) })

	for _, tc := range []struct {
		name   string
		status int
		class  string
	}{
		{name: "accepted", status: http.StatusCreated, class: "accepted"},
		{name: "rejected", status: http.StatusBadRequest, class: "rejected"},
		{name: "gateway error", status: http.StatusBadGateway, class: "gateway_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDAL(t)
			endpoint := "https://push.example.test/secret-subscription"
			s := &apiServer{dal: d, pushHTTPClient: recordingPushClient{status: tc.status}}
			if err := d.PutPushSubscription(testPushSubscription(t, endpoint)); err != nil {
				t.Fatal(err)
			}
			s.enqueueWebPush(webPushPayload{Kind: "chat", ChatID: "c-1", Title: "new", Body: "message"})
			want := "[push] delivery status=" + strconv.Itoa(tc.status) + " class=" + tc.class
			waitForLog(t, &logs, want)
			if strings.Contains(logs.String(), endpoint) {
				t.Fatal("push log must not disclose a subscription endpoint")
			}
		})
	}

	t.Run("transport error is safe", func(t *testing.T) {
		d := newTestDAL(t)
		endpoint := "https://push.example.test/secret-subscription"
		s := &apiServer{dal: d, pushHTTPClient: recordingPushClient{err: errors.New("post " + endpoint + ": connection reset")}}
		if err := d.PutPushSubscription(testPushSubscription(t, endpoint)); err != nil {
			t.Fatal(err)
		}
		s.enqueueWebPush(webPushPayload{Kind: "chat", ChatID: "c-1", Title: "new", Body: "message"})
		waitForLog(t, &logs, "[push] delivery error_class=send_error")
		if strings.Contains(logs.String(), endpoint) {
			t.Fatal("push log must not disclose a subscription endpoint")
		}
	})
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
