package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type legatorAPIClient struct {
	baseURL    string
	bearer     string
	httpClient *http.Client
}

func tryAPIClient() (*legatorAPIClient, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false, fmt.Errorf("failed to resolve home directory: %w", err)
	}

	path := filepath.Join(home, ".config", "legator", "token.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read token cache %s: %w", path, err)
	}

	var tok tokenCache
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, false, fmt.Errorf("failed to parse token cache %s: %w", path, err)
	}
	if strings.TrimSpace(tok.AccessToken) == "" {
		return nil, false, fmt.Errorf("token cache %s has no access_token; run 'legator login'", path)
	}
	if !tok.ExpiresAt.IsZero() && time.Now().After(tok.ExpiresAt.Add(-30*time.Second)) {
		return nil, false, fmt.Errorf("cached token is expired; run 'legator login' again")
	}

	apiURL := strings.TrimSpace(tok.APIURL)
	if apiURL == "" {
		apiURL = envOr("LEGATOR_API_URL", "")
	}
	if apiURL == "" {
		return nil, false, errors.New("token cache missing api_url; run 'legator login --api-url <url>'")
	}

	return &legatorAPIClient{
		baseURL: strings.TrimSuffix(apiURL, "/"),
		bearer:  strings.TrimSpace(tok.AccessToken),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}, true, nil
}

func (c *legatorAPIClient) getJSON(path string, out any) error {
	return c.doJSON(http.MethodGet, path, nil, out)
}

func (c *legatorAPIClient) postJSON(path string, body any, out any) error {
	return c.doJSON(http.MethodPost, path, body, out)
}

func (c *legatorAPIClient) doJSON(method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to encode request body: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("api unauthorized (%d): %s; run 'legator login'", resp.StatusCode, msg)
		}
		return fmt.Errorf("api error (%d): %s", resp.StatusCode, msg)
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("failed to parse api response: %w", err)
	}
	return nil
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}
