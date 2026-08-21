package main

// worker_model_floor_ted79_test.go — T-ed79 parity #2: 「改 model」 is ONE act
// with two faces, so it must have ONE floor.
//
// owner 2026-08-21 (rc-376a41719e62), verbatim: 「如果原本正職可以改 model 外包
// 就應該可以改，如果只有 mira 可以改，那就不變，正職跟外包一樣，mira 是特殊的意義，
// 他代替 owner 執行高權限動作。」
//
// The two faces are PATCH /api/members/{member_id} (update_member) and
// POST /api/outsource-workers/{id}/model (set_outsource_worker_model). Until
// this ticket they sat two rungs apart — machine vs admin_agent — and NOTHING
// in the suite compared them, which is why the gap survived two permission
// audits. This file compares them, in both directions: raising the worker row
// back to admin_agent reddens it, and lowering the member row would too.
//
// ⚠️ It deliberately does NOT assert the OTHER four worker lifecycle rows.
// refocus / relocate / stop / restart were already aligned with their staff
// twins at admin_agent (T-6020, owner 2026-07-26) and the 2026-08-21 ruling
// left them alone; pinning them here would tie an unrelated ruling to this one.

import "testing"

// theTwoFacesOfChangingAModel — the member face first, because it is the one
// the ruling calls the reference ("如果原本正職可以改 model").
var theTwoFacesOfChangingAModel = [][2]string{
	{"PATCH", "/api/members/{member_id}"},
	{"POST", "/api/outsource-workers/{id}/model"},
}

func modelFloorRouteIndex(t *testing.T) map[[2]string]RouteSpec {
	t.Helper()
	specs := defaultRouteSpecs()
	if len(specs) == 0 {
		t.Fatalf("empty route table — every assertion below would be vacuous")
	}
	index := make(map[[2]string]RouteSpec, len(specs))
	for _, s := range specs {
		index[[2]string{s.Method, s.Path}] = s
	}
	return index
}

func TestChangingAModelHasTheSameFloorForStaffAndOutsource(t *testing.T) {
	index := modelFloorRouteIndex(t)
	floors := map[[2]string]string{}
	for _, key := range theTwoFacesOfChangingAModel {
		spec, ok := index[key]
		if !ok {
			t.Fatalf("%s %s is not in the route table at all — this guard compares two "+
				"rows and one of them is gone", key[0], key[1])
		}
		floors[key] = spec.Requires
	}
	staff := floors[theTwoFacesOfChangingAModel[0]]
	worker := floors[theTwoFacesOfChangingAModel[1]]
	if staff != worker {
		t.Errorf("changing a model declares Requires=%q for a staff member and %q for an "+
			"outsource worker. owner 2026-08-21 (rc-376a41719e62): 「如果原本正職可以改 "+
			"model 外包就應該可以改」— one act, one floor. Whichever row moved, move it back.",
			staff, worker)
	}
	if staff != principalMachine {
		t.Errorf("the shared floor is %q, want %q. The staff row's floor is the reference "+
			"the ruling names (T-5336, owner 2026-07-27 kept update_member at the machine "+
			"floor), and the worker row was brought DOWN to meet it — not the other way "+
			"round. Raising this needs a fresh owner ruling.", staff, principalMachine)
	}
}

// A floor is only a floor if an ordinary caller clears it. This is the half the
// route-table comparison above cannot state: two rows could agree on a floor
// that still locks out every plain agent.
func TestAPlainAgentCanChangeEitherKindOfModel(t *testing.T) {
	index := modelFloorRouteIndex(t)
	for _, key := range theTwoFacesOfChangingAModel {
		spec := index[key]
		if !principalAtLeast(principalAgent, spec.Requires) {
			t.Errorf("%s %s requires %q, which a plain %q principal does not clear — a "+
				"roster member cannot change this model. mira (admin_agent) is the "+
				"OWNER's proxy for high-privilege acts, and the owner ruled this is not "+
				"one of them.", key[0], key[1], spec.Requires, principalAgent)
		}
	}
}
