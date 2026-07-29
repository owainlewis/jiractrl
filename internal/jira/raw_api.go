package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

const (
	MaxRawAPIRequestBytes  = 1 << 20
	MaxRawAPIResponseBytes = 8 << 20
	maxRawAPIPathBytes     = 8 << 10
)

var protectedRawRequestHeaders = map[string]bool{
	"authorization":          true,
	"proxy-authorization":    true,
	"proxy":                  true,
	"proxy-connection":       true,
	"cookie":                 true,
	"host":                   true,
	"content-length":         true,
	"transfer-encoding":      true,
	"connection":             true,
	"keep-alive":             true,
	"trailer":                true,
	"upgrade":                true,
	"expect":                 true,
	"te":                     true,
	"forwarded":              true,
	"origin":                 true,
	"referer":                true,
	"x-atlassian-token":      true,
	"x-http-method":          true,
	"x-http-method-override": true,
	"x-method-override":      true,
	"x-original-method":      true,
}

var protectedRawResponseHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"set-cookie":          true,
	"www-authenticate":    true,
	"proxy-authenticate":  true,
}

func (c *Client) RawAPI(ctx context.Context, input RawAPIRequest) (*RawAPIResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if !rawAPIMethodAllowed(method) {
		return nil, &ValidationError{
			Field: "method", Message: "must be GET, HEAD, POST, PUT, PATCH, or DELETE",
		}
	}
	write := method == http.MethodPost || method == http.MethodPut ||
		method == http.MethodPatch || method == http.MethodDelete
	if write && !input.AllowWrite {
		return nil, &ValidationError{Field: "allow-write", Message: "raw API writes require explicit confirmation"}
	}
	if !write && input.AllowWrite {
		return nil, &ValidationError{Field: "allow-write", Message: "is only valid for write methods"}
	}
	safePath, err := validateRawAPIPath(input.Path)
	if err != nil {
		return nil, err
	}
	headers, err := validateRawAPIHeaders(input.Headers)
	if err != nil {
		return nil, err
	}
	if (method == http.MethodGet || method == http.MethodHead) && len(input.Body) > 0 {
		return nil, &ValidationError{Field: "input", Message: method + " does not accept a request body"}
	}
	if len(input.Body) > MaxRawAPIRequestBytes {
		return nil, &ValidationError{
			Field: "input", Message: fmt.Sprintf("exceeds the %d-byte limit", MaxRawAPIRequestBytes),
		}
	}
	if len(input.Body) > 0 && !json.Valid(input.Body) {
		return nil, &ValidationError{Field: "input", Message: "must contain valid JSON"}
	}
	if contentType := headers.Get("Content-Type"); len(input.Body) > 0 &&
		contentType != "" && !isJSONContentType(contentType) {
		return nil, &ValidationError{Field: "header", Message: "Content-Type must be application/json or a +json media type"}
	}
	if _, err := c.detectDeployment(ctx, c.fetchServerInfoWithoutRedirects); err != nil {
		return nil, err
	}

	attempts := 1
	if method == http.MethodGet || method == http.MethodHead {
		attempts = c.retry.MaxAttempts
		if attempts < 1 {
			attempts = 1
		}
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		result, err := c.rawAPIAttempt(ctx, method, safePath, headers, input.Body)
		if err == nil {
			return result, nil
		}
		var jiraErr *Error
		retryable := errors.As(err, &jiraErr) &&
			(jiraErr.StatusCode == http.StatusTooManyRequests || retryableServerStatus(jiraErr.StatusCode))
		if jiraErr != nil {
			jiraErr.Attempts = attempt
		}
		if !retryable || attempt == attempts {
			return nil, err
		}
		if err := c.sleep(ctx, c.retryDelay(attempt, jiraErr)); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("raw API request exhausted without a result")
}

func (c *Client) rawAPIAttempt(ctx context.Context, method, safePath string, headers http.Header, body []byte) (*RawAPIResponse, error) {
	var requestBody io.Reader
	if len(body) > 0 {
		requestBody = bytes.NewReader(body)
	}
	target, err := c.rawAPITarget(safePath)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, target, requestBody)
	if err != nil {
		return nil, err
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	if request.Header.Get("Accept") == "" {
		request.Header.Set("Accept", "application/json")
	}
	if request.Header.Get("Accept-Encoding") == "" {
		request.Header.Set("Accept-Encoding", "identity")
	}
	if len(body) > 0 && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	c.applyAuth(request, c.authMode())

	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, MaxRawAPIResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(responseBody) > MaxRawAPIResponseBytes {
		return nil, fmt.Errorf("raw API response exceeds the %d-byte limit", MaxRawAPIResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, c.responseError(response, responseBody)
	}

	result := &RawAPIResponse{
		Method:      method,
		Path:        safePath,
		Status:      response.StatusCode,
		ContentType: response.Header.Get("Content-Type"),
		Headers:     safeRawResponseHeaders(response.Header),
		Bytes:       len(responseBody),
	}
	if len(responseBody) == 0 {
		return result, nil
	}
	if isJSONContentType(result.ContentType) && json.Valid(responseBody) {
		decoder := json.NewDecoder(bytes.NewReader(responseBody))
		decoder.UseNumber()
		if err := decoder.Decode(&result.Body); err != nil {
			return nil, err
		}
		result.JSON = true
	} else {
		result.BodyBase64 = base64.StdEncoding.EncodeToString(responseBody)
	}
	return result, nil
}

func (c *Client) fetchServerInfoWithoutRedirects(ctx context.Context) (*ServerInfo, error) {
	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	probe := &Client{
		baseURL:            c.baseURL,
		token:              c.token,
		email:              c.email,
		deploymentOverride: c.deploymentOverride,
		http:               &httpClient,
		retry:              c.retry,
		sleep:              c.sleep,
		jitter:             c.jitter,
	}
	return probe.fetchServerInfo(ctx)
}

func (c *Client) rawAPITarget(safePath string) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") ||
		base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return "", errors.New("configured Jira base URL is not a valid HTTP(S) origin")
	}
	target, err := url.Parse(c.baseURL + safePath)
	if err != nil || !c.isSameOrigin(target) || target.User != nil || target.Fragment != "" {
		return "", &ValidationError{Field: "path", Message: "does not resolve on the configured Jira origin"}
	}
	return target.String(), nil
}

func validateRawAPIPath(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", &ValidationError{Field: "path", Message: "must be a non-empty relative path without surrounding whitespace"}
	}
	if len(value) > maxRawAPIPathBytes {
		return "", &ValidationError{Field: "path", Message: fmt.Sprintf("exceeds the %d-byte limit", maxRawAPIPathBytes)}
	}
	if strings.ContainsRune(value, 0) || strings.Contains(value, `\`) || strings.Contains(value, "#") {
		return "", &ValidationError{Field: "path", Message: "must not contain NUL bytes, backslashes, or fragments"}
	}
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "", &ValidationError{Field: "path", Message: "must start with exactly one slash"}
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return "", &ValidationError{Field: "path", Message: "is not a valid relative request path"}
	}
	if parsed.IsAbs() || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil ||
		parsed.Opaque != "" || parsed.Fragment != "" {
		return "", &ValidationError{Field: "path", Message: "absolute URLs and authority changes are not allowed"}
	}
	decoded := parsed.EscapedPath()
	for i := 0; i <= len(value); i++ {
		next, err := url.PathUnescape(decoded)
		if err != nil {
			return "", &ValidationError{Field: "path", Message: "contains invalid escaping"}
		}
		decoded = next
		normalized := strings.ReplaceAll(decoded, `\`, "/")
		for _, segment := range strings.Split(normalized, "/") {
			if segment == "." || segment == ".." ||
				strings.HasPrefix(segment, ".;") || strings.HasPrefix(segment, "..;") {
				return "", &ValidationError{Field: "path", Message: "path traversal is not allowed"}
			}
		}
		if !strings.Contains(decoded, "%") {
			break
		}
		if i == len(value) {
			return "", &ValidationError{Field: "path", Message: "contains excessive nested escaping"}
		}
	}
	if strings.HasPrefix(decoded, "//") || strings.ContainsAny(decoded, `\?#`) {
		return "", &ValidationError{Field: "path", Message: "authority changes are not allowed"}
	}
	for _, r := range decoded {
		if r < 0x20 || r == 0x7f {
			return "", &ValidationError{Field: "path", Message: "control characters are not allowed"}
		}
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", &ValidationError{Field: "path", Message: "contains invalid query escaping"}
	}
	for name := range query {
		decodedName, err := fullyDecodeRawAPIComponent(name)
		if err != nil {
			return "", &ValidationError{Field: "path", Message: "contains invalid query escaping"}
		}
		switch strings.ToLower(decodedName) {
		case "_method", "http-method-override", "x-http-method", "x-http-method-override",
			"x-method-override", "x-original-method":
			return "", &ValidationError{Field: "path", Message: "method override query parameters are not allowed"}
		}
	}
	return value, nil
}

func fullyDecodeRawAPIComponent(value string) (string, error) {
	decoded := value
	for i := 0; i <= len(value); i++ {
		next, err := url.QueryUnescape(decoded)
		if err != nil {
			return "", err
		}
		decoded = next
		if !strings.Contains(decoded, "%") {
			return decoded, nil
		}
	}
	return "", errors.New("excessive nested escaping")
}

func validateRawAPIHeaders(values map[string]string) (http.Header, error) {
	headers := make(http.Header, len(values))
	for name, value := range values {
		canonical := http.CanonicalHeaderKey(strings.TrimSpace(name))
		lower := strings.ToLower(canonical)
		if canonical == "" || !validRawHeaderName(canonical) {
			return nil, &ValidationError{Field: "header", Message: fmt.Sprintf("invalid header name %q", name)}
		}
		if protectedRawRequestHeaders[lower] || strings.HasPrefix(lower, "x-forwarded-") ||
			strings.HasPrefix(lower, "x-original-") || strings.HasPrefix(lower, "x-rewrite-") ||
			strings.HasPrefix(lower, "sec-") {
			return nil, &ValidationError{Field: "header", Message: fmt.Sprintf("%s is protected", canonical)}
		}
		if !validRawHeaderValue(value) {
			return nil, &ValidationError{Field: "header", Message: fmt.Sprintf("%s contains prohibited control characters", canonical)}
		}
		headers.Set(canonical, value)
	}
	return headers, nil
}

func validRawHeaderName(value string) bool {
	for _, r := range value {
		if r <= unicode.MaxASCII &&
			(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			continue
		}
		return false
	}
	return value != ""
}

func validRawHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		if (value[i] < 0x20 && value[i] != '\t') || value[i] == 0x7f {
			return false
		}
	}
	return true
}

func rawAPIMethodAllowed(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func safeRawResponseHeaders(header http.Header) map[string][]string {
	result := make(map[string][]string)
	for name, values := range header {
		lower := strings.ToLower(name)
		if protectedRawResponseHeaders[lower] {
			continue
		}
		result[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
	}
	return result
}

func isJSONContentType(value string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}
