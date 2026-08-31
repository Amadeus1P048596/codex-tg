package automation

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Task is the executable subset of a Telegram-owned automation definition.
// Times are persisted as Unix milliseconds but evaluated in the daemon's local
// timezone so RRULE hours continue to mean local wall-clock time.
type Task struct {
	ID                   string
	Name                 string
	Prompt               string
	Status               string
	RRule                string
	CWD                  string
	ProjectID            string
	Model                string
	ReasoningEffort      string
	NotificationPolicy   string
	ExecutionEnvironment string
	Owner                string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// ListTasks returns valid standalone cron tasks from the configured store.
func (s *Store) ListTasks() ([]Task, error) {
	result, err := s.Apply(map[string]any{"mode": "list"})
	if err != nil {
		return nil, err
	}
	items, _ := result["automations"].([]map[string]any)
	tasks := make([]Task, 0, len(items))
	for _, item := range items {
		task, err := taskFromValues(item)
		if err != nil {
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func taskFromValues(values map[string]any) (Task, error) {
	createdMillis, createdOK := int64Value(values["created_at"])
	updatedMillis, updatedOK := int64Value(values["updated_at"])
	if !createdOK {
		return Task{}, fmt.Errorf("automation %q has no valid created_at", valueString(values["id"]))
	}
	if !updatedOK {
		updatedMillis = createdMillis
	}
	task := Task{
		ID:                   valueString(values["id"]),
		Name:                 valueString(values["name"]),
		Prompt:               valueString(values["prompt"]),
		Status:               strings.ToUpper(valueString(values["status"])),
		RRule:                valueString(values["rrule"]),
		CWD:                  valueString(values["cwd"]),
		ProjectID:            valueString(values["project_id"]),
		Model:                valueString(values["model"]),
		ReasoningEffort:      valueString(values["reasoning_effort"]),
		NotificationPolicy:   valueString(values["notification_policy"]),
		ExecutionEnvironment: valueString(values["execution_environment"]),
		Owner:                valueString(values["owner"]),
		CreatedAt:            time.UnixMilli(createdMillis),
		UpdatedAt:            time.UnixMilli(updatedMillis),
	}
	if !validAutomationID(task.ID) || task.Name == "" || task.Prompt == "" {
		return Task{}, fmt.Errorf("automation has incomplete runtime fields")
	}
	if err := validateRRule(task.RRule); err != nil {
		return Task{}, err
	}
	return task, nil
}

func valueString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

type parsedRRule struct {
	freq     string
	interval int
	hour     int
	minute   int
	byHour   bool
	byMinute bool
	days     map[time.Weekday]struct{}
}

// LatestDue returns the most recent scheduled occurrence at or before now. An
// occurrence from before the task's latest edit is deliberately ignored: an
// edit or resume after today's slot takes effect at the next scheduled slot
// instead of unexpectedly backfilling an old run.
func LatestDue(task Task, now time.Time) (time.Time, bool, error) {
	if now.IsZero() {
		return time.Time{}, false, nil
	}
	location := now.Location()
	boundary := task.UpdatedAt
	if boundary.IsZero() || (!task.CreatedAt.IsZero() && boundary.Before(task.CreatedAt)) {
		boundary = task.CreatedAt
	}
	boundary = boundary.In(location)
	rule, err := parseRRule(task.RRule, boundary)
	if err != nil {
		return time.Time{}, false, err
	}

	var due time.Time
	switch rule.freq {
	case "HOURLY":
		due = latestHourlyDue(rule, boundary, now)
	case "DAILY":
		due = latestDailyDue(rule, boundary, now)
	case "WEEKLY":
		due = latestWeeklyDue(rule, boundary, now)
	default:
		return time.Time{}, false, fmt.Errorf("unsupported frequency %q", rule.freq)
	}
	if due.IsZero() || due.Before(boundary) || due.After(now) {
		return time.Time{}, false, nil
	}
	return due, true, nil
}

func parseRRule(value string, defaults time.Time) (parsedRRule, error) {
	if err := validateRRule(value); err != nil {
		return parsedRRule{}, err
	}
	components := map[string]string{}
	for _, part := range strings.Split(value, ";") {
		key, raw, _ := strings.Cut(part, "=")
		components[strings.ToUpper(strings.TrimSpace(key))] = strings.ToUpper(strings.TrimSpace(raw))
	}
	rule := parsedRRule{
		freq:     components["FREQ"],
		interval: 1,
		hour:     defaults.Hour(),
		minute:   defaults.Minute(),
		days:     map[time.Weekday]struct{}{},
		byHour:   components["BYHOUR"] != "",
		byMinute: components["BYMINUTE"] != "",
	}
	if raw := components["INTERVAL"]; raw != "" {
		rule.interval, _ = strconv.Atoi(raw)
	}
	if rule.byHour {
		rule.hour, _ = strconv.Atoi(components["BYHOUR"])
	}
	if rule.byMinute {
		rule.minute, _ = strconv.Atoi(components["BYMINUTE"])
	}
	for _, raw := range strings.Split(components["BYDAY"], ",") {
		if day, ok := parseWeekday(raw); ok {
			rule.days[day] = struct{}{}
		}
	}
	return rule, nil
}

func latestHourlyDue(rule parsedRRule, boundary, now time.Time) time.Time {
	minute := rule.minute
	candidate := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), minute, 0, 0, now.Location())
	if candidate.After(now) {
		candidate = candidate.Add(-time.Hour)
	}
	anchor := time.Date(boundary.Year(), boundary.Month(), boundary.Day(), boundary.Hour(), 0, 0, 0, boundary.Location())
	maxChecks := rule.interval*24 + 24
	for checked := 0; checked <= maxChecks && !candidate.Before(boundary); checked++ {
		elapsedHours := int(candidate.Sub(anchor) / time.Hour)
		_, dayAllowed := rule.days[candidate.Weekday()]
		if elapsedHours >= 0 && elapsedHours%rule.interval == 0 &&
			(!rule.byHour || candidate.Hour() == rule.hour) && (len(rule.days) == 0 || dayAllowed) {
			return candidate
		}
		candidate = candidate.Add(-time.Hour)
	}
	return time.Time{}
}

func latestDailyDue(rule parsedRRule, boundary, now time.Time) time.Time {
	candidate := time.Date(now.Year(), now.Month(), now.Day(), rule.hour, rule.minute, 0, 0, now.Location())
	if candidate.After(now) {
		candidate = candidate.AddDate(0, 0, -1)
	}
	anchorDay := civilDay(boundary)
	maxChecks := rule.interval*7 + 7
	for checked := 0; checked <= maxChecks && !candidate.Before(boundary); checked++ {
		elapsedDays := civilDay(candidate) - anchorDay
		_, dayAllowed := rule.days[candidate.Weekday()]
		if elapsedDays >= 0 && elapsedDays%rule.interval == 0 && (len(rule.days) == 0 || dayAllowed) {
			return candidate
		}
		candidate = candidate.AddDate(0, 0, -1)
	}
	return time.Time{}
}

func latestWeeklyDue(rule parsedRRule, boundary, now time.Time) time.Time {
	if len(rule.days) == 0 {
		rule.days[boundary.Weekday()] = struct{}{}
	}
	candidate := time.Date(now.Year(), now.Month(), now.Day(), rule.hour, rule.minute, 0, 0, now.Location())
	if candidate.After(now) {
		candidate = candidate.AddDate(0, 0, -1)
	}
	anchorWeek := weekStartDay(boundary)
	maxChecks := rule.interval*7 + 7
	for checked := 0; checked <= maxChecks && !candidate.Before(boundary); checked++ {
		weekDelta := (weekStartDay(candidate) - anchorWeek) / 7
		_, dayAllowed := rule.days[candidate.Weekday()]
		if weekDelta >= 0 && weekDelta%rule.interval == 0 && dayAllowed {
			return candidate
		}
		candidate = candidate.AddDate(0, 0, -1)
	}
	return time.Time{}
}

func civilDay(value time.Time) int {
	year, month, day := value.Date()
	return int(time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Unix() / int64(24*time.Hour/time.Second))
}

func weekStartDay(value time.Time) int {
	day := civilDay(value)
	offset := (int(value.Weekday()) + 6) % 7 // Monday is zero.
	return day - offset
}

func parseWeekday(value string) (time.Weekday, bool) {
	days := map[string]time.Weekday{
		"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
		"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
	}
	day, ok := days[strings.ToUpper(strings.TrimSpace(value))]
	return day, ok
}
