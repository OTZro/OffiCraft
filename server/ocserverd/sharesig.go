package main

// sharesig.go — the station's ?sig= share credentials. Two of them, each with
// its own domain-separated key: the FILE-level one (GET
// /api/chat/attachment/{attachment_id}) at the top of this file, and the
// COMPARISON one (GET /api/diff) at the bottom.
//
// Design (owner-approved minimal version): a share link is the attachment's
// serve URL carrying an HMAC-SHA256 over EXACTLY that attachment id. No
// expiry, no revocation, no stored state — the sig is permanent and grants
// nothing beyond reading the one blob it names (any other id fails the HMAC;
// any other route never consults sigs at all).
//
// KEY: derived from the server signing secret via domain separation
// (SHA-256 over a versioned label + the secret), NEVER the JWT secret used
// raw — a share sig must not be confusable with, or convertible into, any
// JWT-signed material, and the derivation keeps the server stateless (no
// first-boot key mint / DB row; the key is stable exactly as long as the
// signing secret is, matching deriveSecretFromPassword's pattern in jwt.go).

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
)

// shareSigLen truncates the base64url HMAC output: 32 chars = 192 bits,
// far beyond brute force while keeping the URL short.
const shareSigLen = 32

// deriveShareKey domain-separates the share-link HMAC key from the server
// signing secret (same versioned-label construction as the JWT-side
// deriveSecretFromPassword).
func deriveShareKey(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte("officraft.share.hmac.v1:"), secret...))
	return sum[:]
}

// shareSigFor computes the truncated base64url HMAC-SHA256 of one attachment
// id under the derived share key.
func shareSigFor(secret []byte, attachmentID string) string {
	mac := hmac.New(sha256.New, deriveShareKey(secret))
	mac.Write([]byte(attachmentID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:shareSigLen]
}

// verifyShareSig reports whether sig authorizes reading attachmentID —
// constant-time compare, deny on anything else (empty inputs included:
// an empty id/secret still yields a real HMAC the caller cannot guess).
func verifyShareSig(secret []byte, attachmentID, sig string) bool {
	return hmac.Equal([]byte(shareSigFor(secret, attachmentID)), []byte(sig))
}

// ── the comparison link's signature (T-59) ───────────────────────────────────
//
// A comparison is a URL, not an attachment, and it comes in two flavours: the
// INTERNAL one (no sig, a normal logged-in reader) and the EXTERNAL one (a
// server-minted sig, readable with no login at all). Same posture as the
// file-level sig above: no DB row, no expiry, no revocation, verification
// recomputes.
//
// WHAT THE SIGNATURE COVERS: everything that decides what the reader sees —
// both addresses AND both column labels. Leaving the labels out would let a
// recipient relabel a column ("before"/"after" swapped, or a heading that
// misattributes the text) while the sig kept saying the server minted it.
//
// THE KEY IS A DIFFERENT KEY. The version label below is NOT the attachment
// share label, so a diff sig can never be replayed as an attachment sig and
// vice versa — the two grants are different in kind (one blob's bytes vs one
// pair of addresses resolved and rendered), and a shared key would make them
// interchangeable to anyone holding either.
func deriveDiffKey(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte("officraft.diff.hmac.v1:"), secret...))
	return sum[:]
}

// diffSigPayload is the CANONICAL form the HMAC is taken over: all four fields,
// always present (an omitted label signs as empty), percent-encoded and sorted
// by key. url.Values.Encode sorts and escapes, so the payload is a pure
// function of the four values with no ordering or delimiter ambiguity — a value
// containing "&" or "=" cannot be split into a different four.
func diffSigPayload(before, after, labelBefore, labelAfter string) string {
	return url.Values{
		"after":        {after},
		"before":       {before},
		"label_after":  {labelAfter},
		"label_before": {labelBefore},
	}.Encode()
}

// diffSigFor computes the truncated base64url HMAC-SHA256 over that canonical
// form. Same length as the attachment sig (shareSigLen) — the reason is the
// same and there is nothing to gain from two numbers.
func diffSigFor(secret []byte, before, after, labelBefore, labelAfter string) string {
	mac := hmac.New(sha256.New, deriveDiffKey(secret))
	mac.Write([]byte(diffSigPayload(before, after, labelBefore, labelAfter)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:shareSigLen]
}

// verifyDiffSig reports whether sig authorizes reading exactly this comparison
// — constant-time compare, deny on anything else.
func verifyDiffSig(secret []byte, before, after, labelBefore, labelAfter, sig string) bool {
	return hmac.Equal([]byte(diffSigFor(secret, before, after, labelBefore, labelAfter)), []byte(sig))
}
