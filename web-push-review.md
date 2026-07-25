# Web Push candidate review packet

- Candidate base SHA: `cc2cbdbfe944434a9fe165125fa4cde3c6b0a480`
- Candidate commit: this packet is committed with the rebased candidate on `t-8a82-web-push`.
- Landing status: committed on the isolated candidate branch; not merged or deployed. Owner acceptance remains required after deployment.

## Scope

- Server: VAPID key persistence, subscription migration and owner-gated API routes; best-effort chat and reply-card Web Push delivery; expired-subscription pruning.
- Cockpit: PWA manifest/icons, service worker, notification permission and subscription controls, notification click routing to the source chat or Ask card.
- Contract and docs: OpenAPI, frozen route manifest, generated client types, mobile PWA/push guidance.

## Impacted files

`conformance/routes_manifest.json`, `docs/guide/mobile.md`, `frontend/index.html`,
`frontend/public/{apple-touch-icon.png,icon-192.png,icon-512.png,manifest.webmanifest,sw.js}`,
`frontend/src/{App.tsx,api/{adapter.ts,generated/schema.ts,http.ts,mock.ts},components/{PushNotifications.tsx,push-notifications.css,RepliesPage.tsx},i18n/locales/{en.ts,zh.ts},lib/hashRoute.ts}`,
`server/ocserverd/{api_chat.go,api_push.go,api_push_test.go,api_replycards.go,dal.go,go.mod,go.sum,migrations/00033_web_push.sql,ocapi_gen.go,routes.go}`, and `spec/openapi.json`.

## Target validation

- `go test ./...` in `server/ocserverd`
- `go test -v -run 'Test(Push|WebPush)' .` in `server/ocserverd`
- `npm test -- --run src/components/RepliesPage.test.tsx` in `frontend`
- `npm run build` in `frontend`

## Known non-candidate failures

- Full frontend tests: `MonitorPage.hardware-join.test.tsx` expects `17.2%`, while the rendered pre-existing monitor fixture displays `17%`. This is outside the Web Push files and its failure remains after the targeted push/reply tests pass.
- The prior Settings API schema mismatch was authorised for repair because it prevented deployment. `SettingsDTO` now declares the existing `codex_compaction_threshold` server field and both generated clients were refreshed. The frontend production build now passes.

## Independent review

First pass found and this candidate fixes three issues:

1. Chat notification routes now carry both the peer id and message id, so a tap opens the originating conversation and locates the message.
2. Subscription creation accepts only HTTPS public endpoints; delivery resolves only public IPs, refuses redirects, and has a 10-second connection/request deadline.
3. A browser-held subscription is re-saved on startup after permission is granted, repairing a restored or replaced server database.

Independent review: passed after three rounds. The final review confirmed chat routing, subscription recovery, direct-only delivery, proxy disablement, DNS-rebinding defence, RFC6598 rejection, redirect refusal, and 10-second delivery deadlines. No remaining blocker was found.

This candidate is not merged or deployed and must be deployed for owner acceptance before task closure.
