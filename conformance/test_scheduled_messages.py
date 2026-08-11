"""Scheduled-message face — the `custom` cadence's CONDITIONAL requirements.

T-49e7. test_rest_happy pins the happy shapes and the auth matrix pins WHO may
call the four routes; this file pins the one class of rule that lives ONLY in
the spec's prose:

  * `custom_days` / `custom_hours` / `custom_minutes` are REQUIRED when
    `cadence` is `custom`, and an EMPTY set is a 422 rather than a silent "all"
    or a silent "never" — those two readings sit one keystroke apart and are
    indistinguishable on screen;
  * `custom_months` (round 2) is the fourth intersected set and the ONE that may
    be OMITTED — an absent field means all twelve months, which is what keeps
    every client written before the field working unchanged. An explicit `[]` is
    still a 422: absent and empty are different requests and get opposite
    answers, and only the absent one is a shape a slipped keystroke cannot
    produce;
  * their ranges (1-12 / 1-31 / 0-23 / 0-59) are enforced server-side;
  * `hour`/`minute` are required for `daily`/`weekly`/`monthly` and optional
    for `custom` — they left the unconditional required list so a `custom`
    schedule need not send two values it never reads.

🔴 WHY THESE BELONG IN A BLACK-BOX SUITE AND NOT ONLY IN THE SERVER'S OWN
TESTS: none of it is expressible in the OpenAPI schema. "Required according to
the value of another field" and "an array that must not be empty, but only for
one enum value" have no schema form here, so they survive purely as English in
the field descriptions. Every generated client — and every schema validator
pointed at this spec — will therefore accept a request that the server must
refuse, which makes the server the ONLY enforcement point and makes these
refusals a wire behaviour rather than an implementation detail.

The `custom` slot arithmetic itself (the every-20-minutes case, the deliberate
DST divergence where a skipped wall-clock reading is DROPPED rather than moved
forward, and the ambiguous autumn reading firing once) is NOT here and cannot
be: observing it black-box needs either a year of wall clock or a clock
injection this wire does not offer. It is pinned by the server's unit tests
(scheduled_message_custom_t49e7_test.go), which drive the slot functions with
an explicit `now`.
"""

from __future__ import annotations

import pytest

from conftest import AgentIdentity, hire_member, mint_member_token


def _auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


@pytest.fixture(scope="module")
def recipient(client, owner_token) -> AgentIdentity:
    """This module's OWN schedule recipient, so a failed creation here cannot
    perturb another file's agent."""
    member_id = hire_member(client, owner_token, "conf-sched-custom")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    return AgentIdentity(member_id=member_id, token=token, role_key="")


def _create(client, owner_token, recipient: AgentIdentity, **fields):
    body = {"body": "conformance custom schedule", "timezone": "UTC"}
    body.update(fields)
    return client.post(
        f"/api/members/{recipient.member_id}/scheduled-messages",
        json=body, headers=_auth(owner_token),
    )


_FULL_SETS = {"custom_days": [1], "custom_hours": [9], "custom_minutes": [0]}


def _custom(**overrides):
    fields = {"cadence": "custom", **_FULL_SETS}
    fields.update(overrides)
    return fields


def test_custom_accepts_the_three_sets_without_hour_or_minute(
    client, owner_token, recipient
):
    """The positive control the refusals below are measured against.

    Without it, every 422 in this file would also be satisfied by a server that
    refuses `custom` outright — and "the feature is not implemented" would read
    as "validation works".
    """
    r = _create(client, owner_token, recipient, **_custom())
    assert r.status_code == 200, f"want 200, got {r.status_code} {r.text}"
    got = r.json()
    assert got["cadence"] == "custom"
    assert got["custom_days"] == [1]
    assert got["custom_hours"] == [9]
    assert got["custom_minutes"] == [0]
    # hour/minute were never sent; they are the fields `custom` does not read.
    assert got["hour"] == 0 and got["minute"] == 0


@pytest.mark.parametrize(
    "field", ["custom_months", "custom_days", "custom_hours", "custom_minutes"]
)
def test_custom_refuses_an_empty_set(client, owner_token, recipient, field):
    """An empty set is a 422, per set, one case each.

    Refusing it is what keeps "always" and "never" from being one keystroke
    apart: "every day" is expressed by LISTING every day.
    """
    r = _create(client, owner_token, recipient, **_custom(**{field: []}))
    assert r.status_code == 422, (
        f"{field}=[] must be refused, got {r.status_code} {r.text} — "
        "an empty set would land as a schedule with a cadence and no times"
    )


@pytest.mark.parametrize("field", ["custom_days", "custom_hours", "custom_minutes"])
def test_custom_refuses_an_omitted_set(client, owner_token, recipient, field):
    """Omitting one of the three REQUIRED sets is the same refusal as sending an
    empty one.

    They are one requirement, not two: the schema marks them optional (they are
    ignored by every other cadence), so only the server can tell a caller that
    `custom` needs them.

    🔴 `custom_months` is deliberately NOT in this list — omitting it is legal
    and means all twelve months. Adding it here would be a refusal that breaks
    every client written before round 2; the empty-set case above is where it
    belongs.
    """
    fields = _custom()
    fields.pop(field)
    r = _create(client, owner_token, recipient, **fields)
    assert r.status_code == 422, (
        f"omitting {field} for cadence=custom must be refused, "
        f"got {r.status_code} {r.text}"
    )


@pytest.mark.parametrize(
    "field,value",
    [
        ("custom_months", [0]),
        ("custom_months", [13]),
        ("custom_days", [0]),
        ("custom_days", [32]),
        ("custom_hours", [24]),
        ("custom_hours", [25]),
        ("custom_minutes", [60]),
        # A legal value beside an illegal one: the whole set is judged, not
        # just its first element.
        ("custom_days", [1, 32]),
        ("custom_months", [6, 13]),
    ],
)
def test_custom_refuses_out_of_range_set_values(
    client, owner_token, recipient, field, value
):
    r = _create(client, owner_token, recipient, **_custom(**{field: value}))
    assert r.status_code == 422, (
        f"{field}={value} must be refused, got {r.status_code} {r.text} — "
        "an unusable value would be stored for the scheduler to meet later"
    )


def test_custom_accepts_the_ends_of_every_range(client, owner_token, recipient):
    """The sentinel against a well-meaning tighten: day 31, hour 23 and minute
    59 are all schedulable, and so are day 1, hour 0 and minute 0."""
    r = _create(client, owner_token, recipient,
                **_custom(custom_months=[1, 12], custom_days=[1, 31],
                          custom_hours=[0, 23], custom_minutes=[0, 59]))
    assert r.status_code == 200, f"boundary values must be accepted: {r.status_code} {r.text}"


@pytest.mark.parametrize("cadence", ["daily", "weekly", "monthly"])
@pytest.mark.parametrize("omitted", ["hour", "minute"])
def test_calendar_cadences_still_require_the_wall_clock(
    client, owner_token, recipient, cadence, omitted
):
    """`hour`/`minute` left the unconditional required list for `custom`'s sake;
    for every other cadence the requirement is still enforced, here.

    An omitted hour must never be folded to 0: a schedule that silently means
    midnight looks exactly like one that was asked to run at midnight, and
    nothing anywhere would say otherwise.
    """
    fields = {"cadence": cadence, "hour": 9, "minute": 0}
    fields.pop(omitted)
    r = _create(client, owner_token, recipient, **fields)
    assert r.status_code == 422, (
        f"cadence={cadence} without {omitted} must be refused, "
        f"got {r.status_code} {r.text}"
    )


def test_custom_months_omitted_means_every_month(client, owner_token, recipient):
    """The one asymmetry against the other three sets, and the reason round 2
    did not break anyone.

    A client written before `custom_months` sends a body with no month field at
    all — `_FULL_SETS` above is that body, verbatim. Its schedules already meant
    "every month", so the server must accept the create AND land all twelve
    months explicitly. Landing an empty set instead would arm a schedule that
    can never fire again, with no error, no log line and a card that reads
    normally.
    """
    assert "custom_months" not in _FULL_SETS, (
        "the pre-round-2 fixture must not name months, or this test proves nothing"
    )
    r = _create(client, owner_token, recipient, **_custom())
    assert r.status_code == 200, (
        f"a create with no custom_months must be accepted, got {r.status_code} {r.text}"
    )
    assert r.json()["custom_months"] == list(range(1, 13)), (
        f"an omitted custom_months landed as {r.json()['custom_months']!r}, "
        "want every month listed explicitly"
    )


def test_custom_months_stated_are_stored_as_given(client, owner_token, recipient):
    """The positive control for the test above: a stated month set is NOT
    widened to all twelve.

    Without this, "omitted means all twelve" would also pass on a server that
    ignores the field entirely.
    """
    r = _create(client, owner_token, recipient,
                **_custom(custom_months=[9, 3, 12, 3]))
    assert r.status_code == 200, r.text
    assert r.json()["custom_months"] == [3, 9, 12], (
        f"stated months came back as {r.json()['custom_months']!r}, "
        "want the canonical [3, 9, 12] — sorted, deduplicated, and not widened"
    )


def test_the_four_sets_are_always_on_the_response(client, owner_token, recipient):
    """Present for EVERY cadence, as an honest-empty array.

    A field that appears only sometimes forces every reader to distinguish
    "this schedule has no sets" from "this server does not know about sets",
    which are answers to two different questions.
    """
    r = _create(client, owner_token, recipient,
                cadence="daily", hour=9, minute=0)
    assert r.status_code == 200, r.text
    got = r.json()
    for field in ("custom_months", "custom_days", "custom_hours", "custom_minutes"):
        assert field in got, f"{field} is absent from a daily schedule's response"
        assert got[field] == [], f"{field} is {got[field]!r} on a daily schedule, want []"
