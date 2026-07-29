package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/jira"
)

const maxRawAPIInputBytes = jira.MaxRawAPIRequestBytes

func (a App) runAPI(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl api get PATH | api request PATH --method METHOD")
	}
	switch args[0] {
	case "get":
		return a.runAPIRequest(http.MethodGet, args[1:], configPath, true)
	case "request":
		return a.runAPIRequest("", args[1:], configPath, false)
	default:
		return errors.New("usage: jiractrl api get PATH | api request PATH --method METHOD")
	}
}

func (a App) runAPIRequest(fixedMethod string, args []string, configPath string, shorthand bool) error {
	name := "api request"
	if shorthand {
		name = "api get"
	}
	fs := newFlagSet(name)
	methodFlag := fs.String("method", "", "GET, HEAD, POST, PUT, PATCH, or DELETE")
	input := fs.String("input", "", "JSON request body file, or - for stdin")
	allowWrite := fs.Bool("allow-write", false, "confirm a raw POST, PUT, PATCH, or DELETE")
	var headerValues multiFlag
	fs.Var(&headerValues, "header", "safe request header as Name: Value; may be repeated")
	jsonOutput := fs.Bool("json", false, "print structured JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		if shorthand {
			return errors.New("usage: jiractrl api get PATH [--header 'Name: Value'] [--json]")
		}
		return errors.New("usage: jiractrl api request PATH --method METHOD [--input FILE|-] [--header 'Name: Value'] [--allow-write] [--json]")
	}

	method := fixedMethod
	if !shorthand {
		method = strings.ToUpper(strings.TrimSpace(*methodFlag))
		if method == "" {
			return errors.New("api request requires --method")
		}
	} else if strings.TrimSpace(*methodFlag) != "" {
		return errors.New("api get does not accept --method")
	}
	write := method == http.MethodPost || method == http.MethodPut ||
		method == http.MethodPatch || method == http.MethodDelete
	if write && !*allowWrite {
		return errors.New("raw API writes require --allow-write")
	}
	if !write && *allowWrite {
		return errors.New("--allow-write is only valid for POST, PUT, PATCH, or DELETE")
	}
	if (method == http.MethodGet || method == http.MethodHead) && strings.TrimSpace(*input) != "" {
		return fmt.Errorf("%s does not accept --input", method)
	}

	headers, err := parseRawAPIHeaders(headerValues)
	if err != nil {
		return err
	}
	var body []byte
	if strings.TrimSpace(*input) != "" {
		body, err = a.readRawAPIInput(*input)
		if err != nil {
			return err
		}
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.RawAPI(context.Background(), jira.RawAPIRequest{
		Method: method, Path: fs.Arg(0), Headers: headers, Body: body, AllowWrite: *allowWrite,
	})
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	return printRawAPIResponse(a.Stdout, result)
}

func (a App) readRawAPIInput(path string) ([]byte, error) {
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
	data, err := io.ReadAll(io.LimitReader(reader, maxRawAPIInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read --input: %w", err)
	}
	if len(data) > maxRawAPIInputBytes {
		return nil, fmt.Errorf("--input exceeds the %d-byte limit", maxRawAPIInputBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 || !json.Valid(data) {
		return nil, errors.New("--input must contain one valid JSON value")
	}
	return data, nil
}

func parseRawAPIHeaders(values []string) (map[string]string, error) {
	headers := make(map[string]string, len(values))
	for _, value := range values {
		name, headerValue, ok := strings.Cut(value, ":")
		name = strings.TrimSpace(name)
		headerValue = strings.TrimSpace(headerValue)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid --header %q: use 'Name: Value'", value)
		}
		headers[name] = headerValue
	}
	return headers, nil
}

func printRawAPIResponse(w io.Writer, result *jira.RawAPIResponse) error {
	fmt.Fprintf(w, "HTTP %d\n", result.Status)
	if result.ContentType != "" {
		fmt.Fprintf(w, "Content-Type: %s\n", result.ContentType)
	}
	fmt.Fprintf(w, "Bytes: %d\n\n", result.Bytes)
	switch {
	case result.JSON:
		data, err := json.MarshalIndent(result.Body, "", "  ")
		if err != nil {
			return err
		}
		if _, err := w.Write(append(data, '\n')); err != nil {
			return err
		}
	case result.BodyBase64 != "":
		if _, err := base64.StdEncoding.DecodeString(result.BodyBase64); err != nil {
			return err
		}
		fmt.Fprintf(w, "Body-Base64: %s\n", result.BodyBase64)
	}
	return nil
}
