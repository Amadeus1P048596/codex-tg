package automation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const automationFileName = "automation.toml"

var (
	automationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)
	tomlAssignment      = regexp.MustCompile(`^\s*([A-Za-z0-9_-]+)\s*=\s*(.*?)\s*$`)
	allowedRRuleKeys    = map[string]struct{}{
		"FREQ": {}, "INTERVAL": {}, "BYDAY": {}, "BYHOUR": {}, "BYMINUTE": {},
	}
	allowedWeekdays = map[string]struct{}{
		"MO": {}, "TU": {}, "WE": {}, "TH": {}, "FR": {}, "SA": {}, "SU": {},
	}
)

type Store struct {
	root string
	now  func() time.Time
}

func NewStore(root string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{root: filepath.Clean(strings.TrimSpace(root)), now: now}
}

func (s *Store) Apply(args map[string]any) (map[string]any, error) {
	if strings.TrimSpace(s.root) == "" || s.root == "." {
		return nil, errors.New("scheduled tasks directory is not configured")
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, fmt.Errorf("create scheduled tasks directory: %w", err)
	}

	unlock, err := acquireFileLock(s.root, s.now)
	if err != nil {
		return nil, err
	}
	defer unlock()

	mode := strings.ToLower(strings.TrimSpace(stringArg(args, "mode")))
	switch mode {
	case "list":
		return s.list()
	case "view":
		return s.view(stringArg(args, "id"))
	case "create":
		return s.create(args)
	case "update":
		return s.update(args)
	case "delete":
		return s.delete(stringArg(args, "id"))
	default:
		return nil, fmt.Errorf("unsupported mode %q; use list, view, create, update, or delete", mode)
	}
}

func (s *Store) list() (map[string]any, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("list scheduled tasks: %w", err)
	}
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validAutomationID(entry.Name()) {
			continue
		}
		item, err := s.read(entry.Name())
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		left, _ := int64Value(items[i]["updated_at"])
		right, _ := int64Value(items[j]["updated_at"])
		if left == right {
			return fmt.Sprint(items[i]["id"]) < fmt.Sprint(items[j]["id"])
		}
		return left > right
	})
	return map[string]any{"automations": items}, nil
}

func (s *Store) view(id string) (map[string]any, error) {
	if !validAutomationID(id) {
		return nil, errors.New("invalid automation id")
	}
	return s.read(id)
}

func (s *Store) create(args map[string]any) (map[string]any, error) {
	fields, err := validatedCreateFields(args)
	if err != nil {
		return nil, err
	}
	id, err := s.availableID(fields["name"].(string))
	if err != nil {
		return nil, err
	}
	now := s.now().UnixMilli()
	fields["version"] = int64(1)
	fields["id"] = id
	fields["created_at"] = now
	fields["updated_at"] = now

	dir, err := s.taskDir(id)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create automation %q: %w", id, err)
	}
	doc := newAutomationDocument(fields)
	if err := writeDocument(filepath.Join(dir, automationFileName), doc); err != nil {
		_ = os.Remove(dir)
		return nil, err
	}
	return doc.values(), nil
}

func (s *Store) update(args map[string]any) (map[string]any, error) {
	id := strings.TrimSpace(stringArg(args, "id"))
	if !validAutomationID(id) {
		return nil, errors.New("invalid automation id")
	}
	if err := s.ensureExistingTaskDir(id); err != nil {
		return nil, err
	}
	path, err := s.taskFile(id)
	if err != nil {
		return nil, err
	}
	doc, err := readDocument(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("automation %q was not found", id)
		}
		return nil, err
	}
	if storedID, _ := doc.value("id").(string); storedID != id {
		return nil, fmt.Errorf("automation directory %q contains mismatched id", id)
	}
	if kind, _ := doc.value("kind").(string); !strings.EqualFold(kind, "cron") {
		return nil, errors.New("only standalone cron automations can be changed from Telegram")
	}
	if kind, ok := optionalStringArg(args, "kind"); ok && !strings.EqualFold(kind, "cron") {
		return nil, errors.New("Telegram scheduled tasks must use kind=cron; heartbeat tasks cannot target isolated Telegram threads")
	}

	updates := map[string]string{
		"name":                 "name",
		"prompt":               "prompt",
		"rrule":                "rrule",
		"status":               "status",
		"model":                "model",
		"reasoningEffort":      "reasoning_effort",
		"executionEnvironment": "execution_environment",
	}
	for argName, fileName := range updates {
		value, ok := optionalStringArg(args, argName)
		if !ok {
			continue
		}
		if err := validateField(fileName, value); err != nil {
			return nil, err
		}
		doc.set(fileName, value)
	}
	for argName, fileName := range map[string]string{
		"projectId":          "project_id",
		"notificationPolicy": "notification_policy",
	} {
		if _, exists := args[argName]; !exists {
			continue
		}
		value, _ := optionalStringArg(args, argName)
		if value == "" {
			doc.remove(fileName)
			continue
		}
		if err := validateField(fileName, value); err != nil {
			return nil, err
		}
		doc.set(fileName, value)
	}
	doc.set("updated_at", s.now().UnixMilli())
	if err := writeDocument(path, doc); err != nil {
		return nil, err
	}
	return doc.values(), nil
}

func (s *Store) delete(id string) (map[string]any, error) {
	if !validAutomationID(id) {
		return nil, errors.New("invalid automation id")
	}
	item, err := s.read(id)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("automation %q was not found", id)
		}
		return nil, err
	}
	if item["id"] != id {
		return nil, fmt.Errorf("automation directory %q contains mismatched id", id)
	}
	dir, err := s.taskDir(id)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("delete automation %q: %w", id, err)
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

func (s *Store) read(id string) (map[string]any, error) {
	if err := s.ensureExistingTaskDir(id); err != nil {
		return nil, err
	}
	path, err := s.taskFile(id)
	if err != nil {
		return nil, err
	}
	doc, err := readDocument(path)
	if err != nil {
		return nil, err
	}
	values := doc.values()
	if kind, _ := values["kind"].(string); !strings.EqualFold(kind, "cron") {
		return nil, errors.New("only standalone cron automations are visible from Telegram")
	}
	return values, nil
}

func (s *Store) ensureExistingTaskDir(id string) error {
	dir, err := s.taskDir(id)
	if err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("automation %q uses an unsafe linked directory", id)
	}
	if !info.IsDir() {
		return fmt.Errorf("automation %q is not a directory", id)
	}
	return nil
}

func (s *Store) taskDir(id string) (string, error) {
	if !validAutomationID(id) {
		return "", errors.New("invalid automation id")
	}
	dir := filepath.Join(s.root, id)
	rel, err := filepath.Rel(s.root, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", errors.New("automation path escapes the configured directory")
	}
	return dir, nil
}

func (s *Store) taskFile(id string) (string, error) {
	dir, err := s.taskDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, automationFileName), nil
}

func (s *Store) availableID(name string) (string, error) {
	base := automationSlug(name)
	for attempt := 0; attempt < 20; attempt++ {
		candidate := base
		if attempt > 0 {
			suffix, err := randomSuffix(4)
			if err != nil {
				return "", err
			}
			candidate += "-" + suffix
		}
		dir, err := s.taskDir(candidate)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("could not allocate an automation id")
}

func validatedCreateFields(args map[string]any) (map[string]any, error) {
	kind := strings.ToLower(strings.TrimSpace(stringArg(args, "kind")))
	if kind == "" {
		kind = "cron"
	}
	if kind != "cron" {
		return nil, errors.New("Telegram scheduled tasks must use kind=cron; heartbeat tasks cannot target isolated Telegram threads")
	}
	fields := map[string]any{
		"kind":                  "cron",
		"name":                  strings.TrimSpace(stringArg(args, "name")),
		"prompt":                strings.TrimSpace(stringArg(args, "prompt")),
		"rrule":                 strings.TrimSpace(stringArg(args, "rrule")),
		"status":                strings.ToUpper(strings.TrimSpace(stringArg(args, "status"))),
		"execution_environment": "local",
	}
	if fields["status"] == "" {
		fields["status"] = "ACTIVE"
	}
	if _, exists := args["projectId"]; !exists {
		return nil, errors.New("projectId is required; use null for a projectless task")
	}
	for _, key := range []string{"model", "reasoningEffort"} {
		value, ok := optionalStringArg(args, key)
		if !ok || value == "" {
			return nil, fmt.Errorf("%s is required", key)
		}
	}
	for _, pair := range [][2]string{
		{"model", "model"},
		{"reasoningEffort", "reasoning_effort"},
		{"executionEnvironment", "execution_environment"},
		{"projectId", "project_id"},
		{"notificationPolicy", "notification_policy"},
	} {
		if value, ok := optionalStringArg(args, pair[0]); ok && value != "" {
			fields[pair[1]] = value
		}
	}
	for key, value := range fields {
		if text, ok := value.(string); ok {
			if err := validateField(key, text); err != nil {
				return nil, err
			}
		}
	}
	return fields, nil
}

func validateField(name, value string) error {
	value = strings.TrimSpace(value)
	switch name {
	case "name", "prompt":
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	case "kind":
		if !strings.EqualFold(value, "cron") {
			return errors.New("kind must be cron")
		}
	case "status":
		if value != "ACTIVE" && value != "PAUSED" {
			return errors.New("status must be ACTIVE or PAUSED")
		}
	case "rrule":
		return validateRRule(value)
	case "execution_environment":
		if value != "local" {
			return errors.New("executionEnvironment must be local")
		}
	case "notification_policy":
		if value != "failed_runs_only" {
			return errors.New("notificationPolicy must be failed_runs_only or null")
		}
	case "model", "reasoning_effort", "project_id":
		if value == "" {
			return fmt.Errorf("%s cannot be empty", name)
		}
	}
	return nil
}

func validateRRule(value string) error {
	if value == "" {
		return errors.New("rrule is required")
	}
	parts := strings.Split(value, ";")
	seen := map[string]string{}
	for _, part := range parts {
		key, raw, ok := strings.Cut(part, "=")
		key = strings.ToUpper(strings.TrimSpace(key))
		raw = strings.ToUpper(strings.TrimSpace(raw))
		if !ok || key == "" || raw == "" {
			return errors.New("rrule must use KEY=VALUE components")
		}
		if _, ok := allowedRRuleKeys[key]; !ok {
			return fmt.Errorf("rrule component %q is not supported", key)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("rrule component %q is duplicated", key)
		}
		seen[key] = raw
	}
	freq := seen["FREQ"]
	if freq != "HOURLY" && freq != "DAILY" && freq != "WEEKLY" {
		return errors.New("cron rrule FREQ must be HOURLY, DAILY, or WEEKLY")
	}
	if raw := seen["INTERVAL"]; raw != "" {
		interval, err := strconv.Atoi(raw)
		if err != nil || interval < 1 || interval > 365 {
			return errors.New("rrule INTERVAL must be an integer from 1 to 365")
		}
	}
	if raw := seen["BYDAY"]; raw != "" {
		for _, day := range strings.Split(raw, ",") {
			if _, ok := allowedWeekdays[day]; !ok {
				return fmt.Errorf("rrule BYDAY contains invalid weekday %q", day)
			}
		}
	}
	for key, max := range map[string]int{"BYHOUR": 23, "BYMINUTE": 59} {
		if raw := seen[key]; raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 || value > max {
				return fmt.Errorf("rrule %s must be an integer from 0 to %d", key, max)
			}
		}
	}
	return nil
}

func validAutomationID(id string) bool {
	return automationIDPattern.MatchString(strings.TrimSpace(id))
}

func automationSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	lastDash := false
	for _, r := range value {
		allowed := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if allowed {
			out.WriteRune(r)
			lastDash = false
			continue
		}
		if out.Len() > 0 && !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(out.String(), "-")
	if slug == "" {
		slug = "automation"
	}
	if len(slug) > 60 {
		slug = strings.TrimRight(slug[:60], "-")
	}
	return slug
}

func randomSuffix(bytes int) (string, error) {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate automation id: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func stringArg(args map[string]any, key string) string {
	value, _ := optionalStringArg(args, key)
	return value
}

func optionalStringArg(args map[string]any, key string) (string, bool) {
	raw, exists := args[key]
	if !exists || raw == nil {
		return "", exists
	}
	value, ok := raw.(string)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func int64Value(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case float64:
		return int64(typed), true
	default:
		return 0, false
	}
}

type automationDocument struct {
	lines []string
	index map[string]int
}

func newAutomationDocument(fields map[string]any) *automationDocument {
	doc := &automationDocument{index: map[string]int{}}
	order := []string{
		"version", "id", "kind", "name", "prompt", "status", "rrule", "project_id",
		"model", "reasoning_effort", "notification_policy", "execution_environment",
		"created_at", "updated_at",
	}
	for _, key := range order {
		if value, ok := fields[key]; ok {
			doc.set(key, value)
		}
	}
	return doc
}

func readDocument(path string) (*automationDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc := &automationDocument{lines: strings.Split(strings.TrimRight(string(data), "\r\n"), "\n"), index: map[string]int{}}
	for i, line := range doc.lines {
		match := tomlAssignment.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if len(match) == 3 {
			doc.index[match[1]] = i
		}
	}
	return doc, nil
}

func writeDocument(path string, doc *automationDocument) error {
	data := []byte(strings.Join(doc.lines, "\n") + "\n")
	file, err := os.CreateTemp(filepath.Dir(path), ".codex-tg-automation-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary automation file: %w", err)
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure temporary automation file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write automation file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync automation file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close automation file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace automation file: %w", err)
	}
	return os.Chmod(path, 0o600)
}

func (d *automationDocument) set(key string, value any) {
	line := key + " = " + encodeTOMLValue(value)
	if index, ok := d.index[key]; ok {
		d.lines[index] = line
		return
	}
	d.index[key] = len(d.lines)
	d.lines = append(d.lines, line)
}

func (d *automationDocument) remove(key string) {
	index, ok := d.index[key]
	if !ok {
		return
	}
	d.lines = append(d.lines[:index], d.lines[index+1:]...)
	d.index = map[string]int{}
	for i, line := range d.lines {
		match := tomlAssignment.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if len(match) == 3 {
			d.index[match[1]] = i
		}
	}
}

func (d *automationDocument) value(key string) any {
	index, ok := d.index[key]
	if !ok {
		return nil
	}
	match := tomlAssignment.FindStringSubmatch(strings.TrimSuffix(d.lines[index], "\r"))
	if len(match) != 3 {
		return nil
	}
	return decodeTOMLValue(match[2])
}

func (d *automationDocument) values() map[string]any {
	values := map[string]any{}
	for key := range d.index {
		values[key] = d.value(key)
	}
	return values
}

func encodeTOMLValue(value any) string {
	switch typed := value.(type) {
	case string:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case bool:
		return strconv.FormatBool(typed)
	default:
		encoded, _ := json.Marshal(fmt.Sprint(value))
		return string(encoded)
	}
}

func decodeTOMLValue(raw string) any {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'")
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		var value string
		if json.Unmarshal([]byte(raw), &value) == nil {
			return value
		}
	}
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return value
	}
	if value, err := strconv.ParseBool(raw); err == nil {
		return value
	}
	return raw
}

func acquireFileLock(root string, now func() time.Time) (func(), error) {
	path := filepath.Join(root, ".codex-tg-automation.lock")
	for attempt := 0; attempt < 50; attempt++ {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", now().UnixMilli())
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("lock scheduled tasks: %w", err)
		}
		if info, statErr := os.Stat(path); statErr == nil && now().Sub(info.ModTime()) > 30*time.Second {
			_ = os.Remove(path)
			continue
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, errors.New("scheduled tasks are busy; retry shortly")
}
