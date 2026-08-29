package main

// auth_alert.go — the one thing this server does about a credential fact it
// cannot fix by refusing harder.
//
// /api/login has exactly one refusal that is evidence rather than noise: the
// password was CORRECT and the second factor was not. Everything else on that
// route is somebody guessing into the void. This one means the password is out,
// and the answer to a leaked password is not a longer delay — it is changing
// the password. So the server says so, to the one party that is awake and can
// nag the owner about it: the seeded assistant.
//
// 🔴 TWO CONSTRAINTS SHAPE EVERY LINE BELOW, and both are security properties
// rather than tidiness:
//
//	1. IT MUST NOT BE FELT FROM OUTSIDE. The whole reason /api/login answers
//	   every refusal with one sentence and one wall-clock (throttle.go) is that
//	   「密碼對」 and 「密碼錯」 must be indistinguishable. A DB write and an SSE
//	   publish on ONE of those branches would hand that bit straight back in
//	   milliseconds. So the caller's path is a mutex, an increment and a
//	   comparison; the write happens on a goroutine the request never waits for.
//	2. IT MUST NOT BE A MEGAPHONE. The trigger is attacker-controlled — anyone
//	   holding the leaked password can fire it as fast as the brake allows. An
//	   alert per attempt would bury the assistant's chat under thousands of
//	   identical rows and, through her, the owner. So it is rate-limited to one
//	   message per authAlertInterval, and the message carries HOW MANY attempts
//	   it stands for. One row that says 「這段時間有 812 次」 is more useful than
//	   812 rows, not just cheaper.
//
// ⚠️ WHAT THIS IS NOT. It is a message into the assistant's mailbox, delivered
// the same way a scheduled message is (scheduled_message.go) — it does not WAKE
// her. An assistant who is offline reads it when she next comes online, which
// may be long after the fact. Making it a wake would mean an unauthenticated
// caller could start an agent process, which is a worse hole than the one being
// reported. The owner-facing consequence is stated plainly rather than papered
// over: this is a nag, not an alarm, and it is not a substitute for the owner
// noticing.

import (
	"strconv"
	"time"
)

// authAlertInterval is the minimum gap between two 「password accepted, factor
// refused」 alerts. 15 minutes.
//
// It is set by what the reader can act on, not by the attack rate. The action
// this alert asks for — change the password — takes minutes and is the same
// action whether the attempt count is 3 or 3,000, so a second message inside a
// quarter of an hour tells the assistant nothing she is not already acting on.
// It is also the gap that makes the folded count meaningful: shorter and the
// count is always 1, longer and a burst is stale before anyone hears about it.
const authAlertInterval = 15 * time.Minute

// noteFactorRefusedAfterCorrectPassword records one 「password correct, second
// factor wrong」 login refusal and, at most once per authAlertInterval, hands
// the accumulated count to a goroutine that tells the assistant.
//
// 🔴 IT MUST RETURN IN CONSTANT TIME AS FAR AS A CALLER CAN TELL — a lock, an
// increment, a comparison, and at most a `go`. See constraint (1) at the top of
// this file: the caller is on the one login branch whose timing is the secret.
//
// ⚠️ The attempts folded into a suppressed window are reported by the NEXT
// alert, which means the tail of a burst that stops is never reported at all
// (the count sits pending until something fires it). That is deliberate: the
// alternative is a timer that keeps firing after an attack ends, and the owner
// action does not change either way.
func (s *apiServer) noteFactorRefusedAfterCorrectPassword(now time.Time) {
	s.authAlertMu.Lock()
	s.authAlertPending++
	if !s.authAlertLastAt.IsZero() && now.Sub(s.authAlertLastAt) < authAlertInterval {
		s.authAlertMu.Unlock()
		return
	}
	count := s.authAlertPending
	s.authAlertPending = 0
	s.authAlertLastAt = now
	s.authAlertMu.Unlock()

	// 🔴 `go`, NOT a direct call. See constraint (1): a synchronous DB write here
	// would make THIS login refusal slower than the other three and hand back the
	// bit the identical message and the floor are spent hiding. The window is
	// already stamped above, so a flood cannot queue a second goroutine behind
	// this one even if the delivery is slow.
	go s.dispatchAuthAlert(count)
}

// dispatchAuthAlert is the one indirection in this file, and it exists so the
// asynchrony above is FALSIFIABLE rather than merely visible: a test can install
// a deliverer that blocks for seconds and assert the caller still returned
// immediately. Production never sets the field.
func (s *apiServer) dispatchAuthAlert(count int) {
	if s.authAlertDeliver != nil {
		s.authAlertDeliver(count)
		return
	}
	s.deliverPasswordExposedAlert(count)
}

// deliverPasswordExposedAlert writes the durable chat row and fans it, exactly
// the way a scheduled message is delivered (scheduled_message.go): a row the
// recipient owns whether or not anything is connected right now, plus the
// convenience delta for whatever is.
//
// Every fault here is SILENT to the login caller by construction — it runs on
// its own goroutine — so each one is logged instead. A missing assistant is not
// an error: an install whose roster no longer has her has nobody to tell, and
// saying so once per interval is the honest amount of noise.
func (s *apiServer) deliverPasswordExposedAlert(count int) {
	recipient, err := s.resolveChatRecipient(seedMiraID)
	if err != nil {
		outsourceLog("auth-alert: no assistant to warn (%s): %v — the owner will "+
			"NOT be told that their password was accepted with a wrong second factor",
			seedMiraID, err)
		return
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    wireSystemSender,
		Recipient: recipient,
		Body:      passwordExposedAlertBody(count),
		TS:        nowSecs(),
		Meta: map[string]any{
			"auth_alert": map[string]any{
				"kind":     "password_ok_factor_refused",
				"attempts": count,
			},
		},
	}
	if err := s.dal.PutChat(msg); err != nil {
		outsourceLog("auth-alert: durable message to %s failed (the owner will NOT "+
			"be told about %d accepted-password login(s) with a wrong code): %v",
			recipient, count, err)
		return
	}
	// Same convenience payload and audience as every chat delta (spec/sse.md
	// §2.2): both participants plus the owner.
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), triggerServer)
}

// passwordExposedAlertBody is the sentence the assistant reads. Pure, so the
// wording is testable without a database.
//
// It is written for a reader who has to DO something and does not know this
// subsystem: what happened, what it means, what to ask for. It deliberately
// does not say 「有人在攻擊」 — a returning owner whose phone was reset produces
// exactly this signal too, and an alert that overstates its own certainty gets
// ignored the second time.
func passwordExposedAlertBody(count int) string {
	attempts := "1 次"
	if count != 1 {
		attempts = strconv.Itoa(count) + " 次"
	}
	return "🔴 登入警訊：有人用**正確的密碼**登入這台伺服器，但第二階段驗證碼是錯的" +
		"（最近這段時間共 " + attempts + "）。\n\n" +
		"這代表密碼本身已經在別人手上——第二因素目前擋住了他，但密碼不會自己變回安全。" +
		"也有可能是老闆自己換了手機、驗證器還沒重新綁定，兩種情況看起來一模一樣，所以請先問，不要直接下結論。\n\n" +
		"請提醒老闆：確認這幾次是不是他本人，如果不是，請立刻更換密碼" +
		"（個人選單 › 更改密碼）。在他換掉之前，這則提醒每 " +
		strconv.Itoa(int(authAlertInterval.Minutes())) + " 分鐘最多再出現一次，" +
		"次數會累加在同一則裡，所以看到數字變大就是還在持續。"
}
