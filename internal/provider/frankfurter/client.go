package frankfurter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fxrates/internal/domain"
	"fxrates/internal/service"
)

const (
	maxResponseBodyBytes = 64 << 10
)

var errResponseBodyTooLarge = errors.New("response body exceeds size limit")

type Client struct {
	httpClient *http.Client
	baseURL    *url.URL
	retry      RetryPolicy
}

type RetryPolicy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

type retryableError struct {
	err        error
	retryAfter string
}

func (e *retryableError) Error() string {
	return e.err.Error()
}

func (e *retryableError) Unwrap() error {
	return e.err
}

type UpstreamError struct {
	StatusCode int
	Message    string
}

func (e *UpstreamError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("frankfurter returned HTTP status %d", e.StatusCode)
	}

	return fmt.Sprintf("frankfurter returned HTTP status %d: %s", e.StatusCode, e.Message)
}

func New(httpClient *http.Client, baseURL string, retry RetryPolicy) (*Client, error) {
	if httpClient == nil {
		return nil, errors.New("create frankfurter client: HTTP client is required")
	}
	if retry.MaxAttempts <= 0 {
		return nil, errors.New("create frankfurter client: max attempts must be positive")
	}
	if retry.InitialDelay < 0 {
		return nil, errors.New("create frankfurter client: initial retry delay must not be negative")
	}
	if retry.MaxDelay <= 0 {
		return nil, errors.New("create frankfurter client: maximum retry delay must be positive")
	}
	if retry.InitialDelay > retry.MaxDelay {
		return nil, errors.New("create frankfurter client: initial retry delay must not exceed maximum retry delay")
	}

	parsedBaseURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("create frankfurter client: parse base URL: %w", err)
	}
	if (parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https") || parsedBaseURL.Host == "" {
		return nil, errors.New("create frankfurter client: base URL must be an absolute HTTP(S) URL")
	}
	if parsedBaseURL.RawQuery != "" || parsedBaseURL.Fragment != "" {
		return nil, errors.New("create frankfurter client: base URL must not contain a query or fragment")
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    parsedBaseURL,
		retry:      retry,
	}, nil
}

func (c *Client) FetchRate(ctx context.Context, pair domain.Pair) (service.RateSnapshot, error) {
	validatedPair, err := domain.ParsePair(string(pair))
	if err != nil {
		return service.RateSnapshot{}, fmt.Errorf("fetch Frankfurter rate: validate currency pair: %w", err)
	}

	base, quote, _ := strings.Cut(string(validatedPair), "/")
	endpoint := c.baseURL.JoinPath("v2", "rate", base, quote)
	for attempt := 1; attempt <= c.retry.MaxAttempts; attempt++ {
		snapshot, err := c.fetchRateOnce(ctx, endpoint, validatedPair, base, quote)
		if err == nil {
			return snapshot, nil
		}

		var retryErr *retryableError
		if !errors.As(err, &retryErr) || attempt == c.retry.MaxAttempts {
			return service.RateSnapshot{}, err
		}
		delay, delayErr := calculateRetryDelay(c.retry, attempt, retryErr.retryAfter, time.Now())
		if delayErr != nil {
			return service.RateSnapshot{}, fmt.Errorf("fetch Frankfurter rate: %v: %w", delayErr, err)
		}
		if err := waitForRetry(ctx, delay); err != nil {
			return service.RateSnapshot{}, fmt.Errorf("fetch Frankfurter rate: wait before retry: %w", err)
		}
	}

	return service.RateSnapshot{}, errors.New("fetch Frankfurter rate: retry loop ended unexpectedly")
}

func (c *Client) fetchRateOnce(
	ctx context.Context,
	endpoint *url.URL,
	pair domain.Pair,
	base string,
	quote string,
) (service.RateSnapshot, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return service.RateSnapshot{}, fmt.Errorf("fetch Frankfurter rate: create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		err = fmt.Errorf("fetch Frankfurter rate: send request: %w", err)
		if ctx.Err() != nil {
			return service.RateSnapshot{}, err
		}

		return service.RateSnapshot{}, &retryableError{err: err}
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		err := decodeUpstreamError(response)
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
			return service.RateSnapshot{}, &retryableError{
				err:        err,
				retryAfter: response.Header.Get("Retry-After"),
			}
		}

		return service.RateSnapshot{}, err
	}

	body, err := readLimitedBody(response.Body)
	if err != nil {
		err = fmt.Errorf("fetch Frankfurter rate: read response: %w", err)
		if ctx.Err() != nil ||
			errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, errResponseBodyTooLarge) {
			return service.RateSnapshot{}, err
		}

		return service.RateSnapshot{}, &retryableError{err: err}
	}

	var payload rateResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return service.RateSnapshot{}, fmt.Errorf("fetch Frankfurter rate: decode response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return service.RateSnapshot{}, errors.New("fetch Frankfurter rate: response must contain one JSON object")
	}

	if payload.Base != base || payload.Quote != quote {
		return service.RateSnapshot{}, fmt.Errorf(
			"fetch Frankfurter rate: response pair %s/%s does not match requested pair %s/%s",
			payload.Base,
			payload.Quote,
			base,
			quote,
		)
	}

	rate, err := domain.ParseRate(payload.Rate.String())
	if err != nil {
		return service.RateSnapshot{}, fmt.Errorf("fetch Frankfurter rate: response contains an invalid rate: %w", err)
	}

	rateDate, err := time.Parse(time.DateOnly, payload.Date)
	if err != nil {
		return service.RateSnapshot{}, fmt.Errorf("fetch Frankfurter rate: parse rate date: %w", err)
	}

	return service.RateSnapshot{
		Pair:     pair,
		Rate:     rate,
		RateDate: rateDate,
	}, nil
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type rateResponse struct {
	Date  string      `json:"date"`
	Base  string      `json:"base"`
	Quote string      `json:"quote"`
	Rate  json.Number `json:"rate"`
}

type errorResponse struct {
	Message string `json:"message"`
}

func readLimitedBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxResponseBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBodyBytes {
		return nil, fmt.Errorf("%w: limit is %d bytes", errResponseBodyTooLarge, maxResponseBodyBytes)
	}

	return data, nil
}

func decodeUpstreamError(response *http.Response) error {
	body, err := readLimitedBody(response.Body)
	if err != nil {
		return fmt.Errorf("frankfurter returned HTTP status %d: read error response: %w", response.StatusCode, err)
	}

	var payload errorResponse
	_ = json.Unmarshal(body, &payload)

	return &UpstreamError{
		StatusCode: response.StatusCode,
		Message:    strings.TrimSpace(payload.Message),
	}
}

var _ service.RateProvider = (*Client)(nil)
