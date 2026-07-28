package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://jira.example.com"
	DefaultFields  = "summary,status,assignee,priority,issuetype"
	BinaryName     = "jiractrl"

	DefaultRetryMaxAttempts = 3
	DefaultRetryBaseDelay   = 500 * time.Millisecond
	DefaultRetryMaxDelay    = 30 * time.Second
)

type Config struct {
	BaseURL           string
	Token             string
	Email             string
	Deployment        string
	Timeout           time.Duration
	DefaultMaxResults int
	DefaultOutput     string
	RetryMaxAttempts  int
	RetryBaseDelay    time.Duration
	RetryMaxDelay     time.Duration
	Profiles          map[string]Profile

	retryMaxAttemptsSet bool
	retryBaseDelaySet   bool
	retryMaxDelaySet    bool
}

type Profile struct {
	JQL        string
	Fields     []string
	MaxResults int
}

func Load(path string, timeout time.Duration) (Config, error) {
	cfg := Config{
		BaseURL:           DefaultBaseURL,
		Deployment:        "auto",
		Timeout:           timeout,
		DefaultMaxResults: 20,
		DefaultOutput:     "text",
		RetryMaxAttempts:  DefaultRetryMaxAttempts,
		RetryBaseDelay:    DefaultRetryBaseDelay,
		RetryMaxDelay:     DefaultRetryMaxDelay,
		Profiles:          map[string]Profile{},
	}

	if path == "" {
		path = os.Getenv("JIRACTRL_CONFIG")
	}
	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, ".config", BinaryName, "config.toml")
		}
	}
	if path != "" {
		fileCfg, err := ReadFile(path)
		if err == nil {
			cfg = Merge(cfg, fileCfg)
		} else if !errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
	}

	cfg.BaseURL = strings.TrimRight(firstNonEmpty(os.Getenv("JIRACTRL_BASE_URL"), os.Getenv("JIRA_BASE_URL"), cfg.BaseURL), "/")
	cfg.Token = firstNonEmpty(os.Getenv("JIRACTRL_TOKEN"), os.Getenv("JIRA_PAT"), os.Getenv("JIRA_TOKEN"), cfg.Token)
	cfg.Email = firstNonEmpty(os.Getenv("JIRACTRL_EMAIL"), os.Getenv("JIRA_EMAIL"), cfg.Email)
	cfg.Deployment = firstNonEmpty(os.Getenv("JIRACTRL_DEPLOYMENT"), cfg.Deployment, "auto")
	if !validDeployment(cfg.Deployment) {
		return Config{}, fmt.Errorf("invalid Jira deployment %q: use auto, cloud, or data_center", cfg.Deployment)
	}
	if cfg.RetryMaxAttempts < 1 || cfg.RetryMaxAttempts > 10 {
		return Config{}, errors.New("retry.max_attempts must be between 1 and 10")
	}
	if cfg.RetryBaseDelay < 0 || cfg.RetryMaxDelay < 0 || cfg.RetryBaseDelay > cfg.RetryMaxDelay {
		return Config{}, errors.New("retry delays must be non-negative and base_delay_ms must not exceed max_delay_ms")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return Config{}, errors.New("set token in config.toml or JIRACTRL_TOKEN/JIRA_PAT")
	}
	return cfg, nil
}

func ReadFile(path string) (cfg Config, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%v", recovered)
		}
	}()

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg = Config{Profiles: map[string]Profile{}}
	section := ""
	profileName := ""

	for lineNo, rawLine := range strings.Split(string(data), "\n") {
		line := stripTOMLComment(strings.TrimSpace(rawLine))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			profileName = ""
			if strings.HasPrefix(section, "profiles.") {
				profileName = strings.TrimPrefix(section, "profiles.")
				if profileName == "" {
					return Config{}, fmt.Errorf("%s:%d: empty profile name", path, lineNo+1)
				}
				if _, ok := cfg.Profiles[profileName]; !ok {
					cfg.Profiles[profileName] = Profile{}
				}
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("%s:%d: expected key = value", path, lineNo+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch {
		case section == "jira":
			switch key {
			case "base_url":
				cfg.BaseURL = mustParseTOMLString(path, lineNo+1, value)
			case "token":
				cfg.Token = mustParseTOMLString(path, lineNo+1, value)
			case "email":
				cfg.Email = mustParseTOMLString(path, lineNo+1, value)
			case "deployment":
				cfg.Deployment = mustParseTOMLString(path, lineNo+1, value)
			}
		case section == "defaults":
			switch key {
			case "max_results":
				cfg.DefaultMaxResults = mustParseTOMLInt(path, lineNo+1, value)
			case "output":
				cfg.DefaultOutput = mustParseTOMLString(path, lineNo+1, value)
			}
		case section == "retry":
			switch key {
			case "max_attempts":
				cfg.RetryMaxAttempts = mustParseTOMLInt(path, lineNo+1, value)
				cfg.retryMaxAttemptsSet = true
			case "base_delay_ms":
				cfg.RetryBaseDelay = time.Duration(mustParseTOMLInt(path, lineNo+1, value)) * time.Millisecond
				cfg.retryBaseDelaySet = true
			case "max_delay_ms":
				cfg.RetryMaxDelay = time.Duration(mustParseTOMLInt(path, lineNo+1, value)) * time.Millisecond
				cfg.retryMaxDelaySet = true
			}
		case profileName != "":
			p := cfg.Profiles[profileName]
			switch key {
			case "jql":
				p.JQL = mustParseTOMLString(path, lineNo+1, value)
			case "fields":
				p.Fields = mustParseTOMLStringArray(path, lineNo+1, value)
			case "max_results":
				p.MaxResults = mustParseTOMLInt(path, lineNo+1, value)
			}
			cfg.Profiles[profileName] = p
		}
	}

	return cfg, nil
}

func Merge(base, override Config) Config {
	if override.BaseURL != "" {
		base.BaseURL = override.BaseURL
	}
	if override.Token != "" {
		base.Token = override.Token
	}
	if override.Email != "" {
		base.Email = override.Email
	}
	if override.Deployment != "" {
		base.Deployment = override.Deployment
	}
	if override.DefaultMaxResults != 0 {
		base.DefaultMaxResults = override.DefaultMaxResults
	}
	if override.DefaultOutput != "" {
		base.DefaultOutput = override.DefaultOutput
	}
	if override.retryMaxAttemptsSet || override.RetryMaxAttempts != 0 {
		base.RetryMaxAttempts = override.RetryMaxAttempts
	}
	if override.retryBaseDelaySet || override.RetryBaseDelay != 0 {
		base.RetryBaseDelay = override.RetryBaseDelay
	}
	if override.retryMaxDelaySet || override.RetryMaxDelay != 0 {
		base.RetryMaxDelay = override.RetryMaxDelay
	}
	if base.Profiles == nil {
		base.Profiles = map[string]Profile{}
	}
	for name, p := range override.Profiles {
		base.Profiles[name] = p
	}
	return base
}

func validDeployment(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "cloud", "data_center":
		return true
	default:
		return false
	}
}

func stripTOMLComment(line string) string {
	inQuote := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if r == '#' && !inQuote {
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}

func mustParseTOMLString(path string, lineNo int, value string) string {
	parsed, err := strconv.Unquote(value)
	if err != nil {
		panic(fmt.Sprintf("%s:%d: expected quoted string", path, lineNo))
	}
	return parsed
}

func mustParseTOMLInt(path string, lineNo int, value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic(fmt.Sprintf("%s:%d: expected integer", path, lineNo))
	}
	return parsed
}

func mustParseTOMLStringArray(path string, lineNo int, value string) []string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		panic(fmt.Sprintf("%s:%d: expected string array", path, lineNo))
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if body == "" {
		return nil
	}
	var values []string
	for _, part := range splitCommaList(body) {
		values = append(values, mustParseTOMLString(path, lineNo, strings.TrimSpace(part)))
	}
	return values
}

func splitCommaList(value string) []string {
	var parts []string
	start := 0
	inQuote := false
	escaped := false
	for i, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if r == ',' && !inQuote {
			parts = append(parts, value[start:i])
			start = i + 1
		}
	}
	parts = append(parts, value[start:])
	return parts
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
