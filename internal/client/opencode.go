// Package client manages upstream API client connections.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/syxc/oc-go-cc/internal/config"
	"github.com/syxc/oc-go-cc/pkg/types"
)

// OpenCodeClient handles communication with OpenCode Go API.
type OpenCodeClient struct {
	openAIConfig    EndpointConfig
	anthropicConfig EndpointConfig
	httpClient      *http.Client
}

// EndpointConfig holds configuration for a specific API endpoint.
type EndpointConfig struct {
	BaseURL string
	APIKey  string
}

// NewOpenCodeClient creates a new OpenCode Go client.
func NewOpenCodeClient(cfg config.OpenCodeGoConfig, apiKey string) *OpenCodeClient {
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	// Configure connection pooling for better performance
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		MaxConnsPerHost:     50,
		DisableKeepAlives:   false,
	}

	return &OpenCodeClient{
		openAIConfig: EndpointConfig{
			BaseURL: cfg.BaseURL,
			APIKey:  apiKey,
		},
		anthropicConfig: EndpointConfig{
			BaseURL: cfg.AnthropicBaseURL,
			APIKey:  apiKey,
		},
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

// ModelInfo holds metadata for a single model returned by the upstream API.
type ModelInfo struct {
	ID    string `json:"id"`
	Owned string `json:"owned_by,omitempty"`
}

// ListModels queries the upstream /v1/models endpoint and returns the available models.
// It uses the OpenAI base URL; callers should fall back gracefully if the endpoint
// is unreachable.
func (c *OpenCodeClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	// Derive the models list URL from the chat completions base URL.
	// base_url is typically "https://host/v1/chat/completions" — strip the last two segments.
	modelsURL := strings.TrimRight(c.openAIConfig.BaseURL, "/")
	if idx := strings.LastIndex(modelsURL, "/"); idx != -1 {
		modelsURL = modelsURL[:idx] // strip "/chat/completions" → "/v1"
	}
	modelsURL += "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.openAIConfig.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("models request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("models request returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}
	return result.Data, nil
}

// IsAnthropicModel is an alias kept for backward compatibility with external callers.
// Prefer config.IsLegacyAnthropicModel or ModelConfig.ShouldForwardRaw().
func IsAnthropicModel(modelID string) bool {
	return config.IsLegacyAnthropicModel(modelID)
}

// getEndpoint returns the appropriate endpoint config for a model.
func (c *OpenCodeClient) getEndpoint(modelID string) EndpointConfig {
	if config.IsLegacyAnthropicModel(modelID) {
		return c.anthropicConfig
	}
	return c.openAIConfig
}

// ChatCompletion sends a chat completion request to the OpenCode Go API.
// Returns the raw HTTP response for the caller to handle (streaming or body read).
func (c *OpenCodeClient) ChatCompletion(
	ctx context.Context,
	modelID string,
	req *types.ChatCompletionRequest,
) (*http.Response, error) {
	endpoint := c.getEndpoint(modelID)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+endpoint.APIKey)

	// Add streaming header if requested
	if req.Stream != nil && *req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Check for error status codes
	if resp.StatusCode >= http.StatusBadRequest {
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return resp, nil
}

// ChatCompletionNonStreaming sends a non-streaming request and returns the full parsed response.
func (c *OpenCodeClient) ChatCompletionNonStreaming(
	ctx context.Context,
	modelID string,
	req *types.ChatCompletionRequest,
) (*types.ChatCompletionResponse, error) {
	// Force non-streaming
	streamFalse := false
	req.Stream = &streamFalse

	resp, err := c.ChatCompletion(ctx, modelID, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var chatResp types.ChatCompletionResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &chatResp, nil
}

// GetStreamingBody returns the response body for streaming consumption.
// The caller is responsible for closing the returned ReadCloser.
func (c *OpenCodeClient) GetStreamingBody(
	ctx context.Context,
	modelID string,
	req *types.ChatCompletionRequest,
) (io.ReadCloser, error) {
	// Force streaming
	streamTrue := true
	req.Stream = &streamTrue

	resp, err := c.ChatCompletion(ctx, modelID, req)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

// SendAnthropicRequest sends a raw Anthropic-format request (for MiniMax models).
// This skips the OpenAI transformation entirely.
func (c *OpenCodeClient) SendAnthropicRequest(
	ctx context.Context,
	body []byte,
	stream bool,
) (*http.Response, error) {
	endpoint := c.anthropicConfig

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	// Incase OpenCode Go expects x-api-key instead
	httpReq.Header.Set("x-api-key", endpoint.APIKey)

	// Add streaming header if requested
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Check for error status codes
	if resp.StatusCode >= http.StatusBadRequest {
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return resp, nil
}
