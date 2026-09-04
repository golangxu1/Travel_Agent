package domain

import "testing"

func validPlanRequest() PlanRequest {
	return PlanRequest{
		Origin:      "上海",
		Destination: "杭州",
		StartDate:   "2026-10-01",
		EndDate:     "2026-10-03",
		Transport:   TransportMixed,
		Preferences: []string{TravelStyleScenery},
		People:      2,
	}
}

func TestValidatePlanAcceptsLegacyDefaults(t *testing.T) {
	if err := ValidatePlan(validPlanRequest()); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}
}

func TestValidatePlanRejectsInvalidDateRange(t *testing.T) {
	req := validPlanRequest()
	req.EndDate = "2026-09-30"
	if err := ValidatePlan(req); err == nil {
		t.Fatal("expected date range validation error")
	}
}

func TestValidatePlanRejectsLongTrip(t *testing.T) {
	req := validPlanRequest()
	req.EndDate = "2026-12-01"
	if err := ValidatePlan(req); err == nil {
		t.Fatal("expected max trip length validation error")
	}
}

func TestValidateRegenerateRejectsOversizedContext(t *testing.T) {
	req := RegenerateRequest{PlanRequest: validPlanRequest(), Module: "itinerary", Feedback: "请调整"}
	req.Context = map[string]any{"weather": string(make([]rune, MaxContextValueLen+1))}
	if err := ValidateRegenerate(req); err == nil {
		t.Fatal("expected context size validation error")
	}
}
