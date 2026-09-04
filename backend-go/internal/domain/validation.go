package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxTripDays        = 31
	MaxLocationLength  = 100
	MaxFeedbackLength  = 2000
	MaxContextEntries  = 12
	MaxContextKeyLen   = 64
	MaxContextValueLen = 6000
)

// ValidationError is deliberately small and stable so handlers can expose a
// safe client-facing message without leaking provider or implementation errors.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func invalid(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

func validTransport(value string) bool {
	switch value {
	case TransportSelfDrive, TransportPublic, TransportMixed:
		return true
	default:
		return false
	}
}

func validTravelStyle(value string) bool {
	switch value {
	case TravelStyleFood, TravelStyleCulture, TravelStyleScenery,
		TravelStyleShopping, TravelStyleRelaxation, TravelStyleAdventure:
		return true
	default:
		return false
	}
}

func validBudget(value string) bool {
	switch value {
	case BudgetEconomy, BudgetMiddle, BudgetLuxury:
		return true
	default:
		return false
	}
}

func validatePlanFields(req PlanRequest) error {
	if strings.TrimSpace(req.Origin) == "" {
		return invalid("origin", "不能为空")
	}
	if runeLen(req.Origin) > MaxLocationLength {
		return invalid("origin", fmt.Sprintf("长度不能超过 %d 个字符", MaxLocationLength))
	}
	if strings.TrimSpace(req.Destination) == "" {
		return invalid("destination", "不能为空")
	}
	if runeLen(req.Destination) > MaxLocationLength {
		return invalid("destination", fmt.Sprintf("长度不能超过 %d 个字符", MaxLocationLength))
	}

	start, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		return invalid("start_date", "必须是 YYYY-MM-DD 格式的日期")
	}
	end, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		return invalid("end_date", "必须是 YYYY-MM-DD 格式的日期")
	}
	if end.Before(start) {
		return invalid("end_date", "不能早于 start_date")
	}
	if int(end.Sub(start).Hours()/24)+1 > MaxTripDays {
		return invalid("end_date", fmt.Sprintf("行程不能超过 %d 天", MaxTripDays))
	}

	if !validTransport(req.Transport) {
		return invalid("transport", "不支持的出行方式")
	}
	if len(req.Preferences) == 0 {
		return invalid("preferences", "至少选择一个旅行偏好")
	}
	if len(req.Preferences) > 6 {
		return invalid("preferences", "旅行偏好数量不能超过 6 个")
	}
	seen := make(map[string]struct{}, len(req.Preferences))
	for i, preference := range req.Preferences {
		if !validTravelStyle(preference) {
			return invalid(fmt.Sprintf("preferences[%d]", i), "不支持的旅行偏好")
		}
		if _, ok := seen[preference]; ok {
			return invalid("preferences", "不能包含重复值")
		}
		seen[preference] = struct{}{}
	}
	if req.People < 1 || req.People > 20 {
		return invalid("people", "必须在 1 到 20 之间")
	}
	if req.BudgetLevel != nil {
		if !validBudget(*req.BudgetLevel) {
			return invalid("budget_level", "不支持的预算档次")
		}
	}
	return nil
}

// ValidatePlan validates a normalized plan request.
func ValidatePlan(req PlanRequest) error {
	return validatePlanFields(req)
}

// ValidateRegenerate validates both the shared plan fields and regeneration
// specific limits. Context values are intentionally bounded because they are
// fed into downstream prompts.
func ValidateRegenerate(req RegenerateRequest) error {
	if err := validatePlanFields(req.PlanRequest); err != nil {
		return err
	}
	switch req.Module {
	case "weather", "destination", "accommodation", "itinerary", "budget":
	default:
		return invalid("module", "不支持的模块")
	}
	if strings.TrimSpace(req.Feedback) == "" {
		return invalid("feedback", "不能为空")
	}
	if runeLen(req.Feedback) > MaxFeedbackLength {
		return invalid("feedback", fmt.Sprintf("长度不能超过 %d 个字符", MaxFeedbackLength))
	}
	if len(req.Context) > MaxContextEntries {
		return invalid("context", fmt.Sprintf("条目数不能超过 %d", MaxContextEntries))
	}
	for key, value := range req.Context {
		if strings.TrimSpace(key) == "" || runeLen(key) > MaxContextKeyLen {
			return invalid("context", "键不能为空且长度不能超过限制")
		}
		if runeLen(fmt.Sprint(value)) > MaxContextValueLen {
			return invalid("context."+key, fmt.Sprintf("长度不能超过 %d 个字符", MaxContextValueLen))
		}
	}
	return nil
}

func runeLen(value string) int {
	if !utf8.ValidString(value) {
		return len(value)
	}
	return utf8.RuneCountInString(value)
}
