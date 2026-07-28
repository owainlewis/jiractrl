package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/owainlewis/jiractrl/internal/jira"
)

const maxMutationInputBytes = 1 << 20

func (a App) readMutationInput(path string) (map[string]any, error) {
	var reader io.Reader
	var file *os.File
	if path == "-" {
		reader = a.Stdin
		if reader == nil {
			reader = os.Stdin
		}
	} else {
		var err error
		file, err = os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read --input: %w", err)
		}
		defer file.Close()
		reader = file
	}

	data, err := io.ReadAll(io.LimitReader(reader, maxMutationInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read --input: %w", err)
	}
	if len(data) > maxMutationInputBytes {
		return nil, fmt.Errorf("--input exceeds the %d-byte limit", maxMutationInputBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse --input JSON: %w", err)
	}
	if payload == nil {
		return nil, errors.New("--input must contain one JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("--input must contain exactly one JSON object")
		}
		return nil, fmt.Errorf("parse trailing --input JSON: %w", err)
	}
	return payload, nil
}

func validateMutationEnvelope(command string, payload map[string]any) error {
	allowed := map[string]bool{
		"fields":          true,
		"update":          true,
		"properties":      true,
		"historyMetadata": true,
	}
	if command == "create" || command == "transition" {
		allowed["transition"] = true
	}
	for key := range payload {
		if !allowed[key] {
			return fmt.Errorf("--input field %q is not allowed for %s", key, command)
		}
	}

	for _, key := range []string{"fields", "update", "historyMetadata"} {
		if value, ok := payload[key]; ok {
			if _, ok := value.(map[string]any); !ok {
				return fmt.Errorf("--input %s must be a JSON object", key)
			}
		}
	}
	if value, ok := payload["properties"]; ok {
		if _, ok := value.([]any); !ok {
			return errors.New("--input properties must be a JSON array")
		}
	}
	if value, ok := payload["transition"]; ok {
		transition, ok := value.(map[string]any)
		if !ok {
			return errors.New("--input transition must be a JSON object")
		}
		id, ok := transition["id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return errors.New("--input transition.id must be a non-empty string")
		}
	}

	switch command {
	case "create":
		if _, ok := payload["fields"]; !ok {
			return errors.New("--input for create requires a fields object")
		}
	case "update":
		if len(payload) == 0 {
			return errors.New("--input for update must contain fields, update, properties, or historyMetadata")
		}
	case "transition":
		if len(payload) == 0 {
			return errors.New("--input for transition must contain transition, fields, update, properties, or historyMetadata")
		}
	default:
		return fmt.Errorf("unsupported mutation command %q", command)
	}
	return nil
}

func writeDryRun(w io.Writer, request *jira.MutationRequest) error {
	return writeSuccessJSON(w, map[string]any{
		"dryRun":  true,
		"request": request,
	})
}
