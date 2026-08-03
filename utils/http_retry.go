package utils

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/imroc/req/v3"
)

const (
	maxHTTPAttempts   = 6
	perAttemptTimeout = 15 * time.Minute
)

var (
	retryBaseDelay = 2 * time.Second
	retryMaxDelay  = 30 * time.Second
)

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusNotFound ||
		code >= 500
}

func backoffFor(attempt int) time.Duration {
	delay := retryBaseDelay << (attempt - 1)
	if delay > retryMaxDelay || delay <= 0 {
		delay = retryMaxDelay
	}
	return delay
}

func newHTTPClient() *req.Client {
	client := req.C().
		SetTimeout(perAttemptTimeout).
		DisableAutoDecode().
		SetProxy(http.ProxyFromEnvironment).
		SetCommonRetryCount(maxHTTPAttempts - 1).
		SetCommonRetryCondition(func(resp *req.Response, err error) bool {
			if err != nil {
				return true
			}
			return resp != nil && resp.Response != nil && retryableStatus(resp.StatusCode)
		}).
		SetCommonRetryInterval(func(_ *req.Response, attempt int) time.Duration {
			return backoffFor(attempt)
		}).
		SetCommonRetryHook(func(resp *req.Response, err error) {
			if resp == nil || resp.Request == nil {
				return
			}
			retryErr := err
			if retryErr == nil && resp.Response != nil {
				retryErr = &httpStatusError{
					url:  resp.Request.RawURL,
					code: resp.StatusCode,
					body: resp.String(),
				}
			}
			log.Printf(
				"Failed to load %s, retrying in %s... (attempt %d/%d): %v",
				resp.Request.RawURL,
				backoffFor(resp.Request.RetryAttempt),
				resp.Request.RetryAttempt+1,
				maxHTTPAttempts,
				retryErr,
			)
		})

	client.Transport.
		SetMaxIdleConns(200).
		SetMaxConnsPerHost(50).
		SetIdleConnTimeout(90 * time.Second).
		SetTLSHandshakeTimeout(30 * time.Second).
		SetResponseHeaderTimeout(60 * time.Second).
		SetExpectContinueTimeout(time.Second)
	client.Transport.MaxIdleConnsPerHost = 50

	return client
}

var sharedHTTPClient = newHTTPClient()

// FetchWithRetry performs a GET with bounded retry and returns the response body.
func FetchWithRetry(urlStr string, bearerToken *string) ([]byte, error) {
	if _, err := url.ParseRequestURI(urlStr); err != nil {
		return nil, err
	}

	request := sharedHTTPClient.R()
	if bearerToken != nil {
		request.SetBearerAuthToken(*bearerToken)
	}
	resp, err := request.Get(urlStr)
	return responseBody(urlStr, resp, err)
}

// PostFormWithRetry performs a form POST with the same retry policy as FetchWithRetry.
func PostFormWithRetry(urlStr string, data url.Values) ([]byte, error) {
	if _, err := url.ParseRequestURI(urlStr); err != nil {
		return nil, err
	}

	resp, err := sharedHTTPClient.R().
		SetFormDataFromValues(data).
		Post(urlStr)
	return responseBody(urlStr, resp, err)
}

func responseBody(urlStr string, resp *req.Response, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{
			url:  urlStr,
			code: resp.StatusCode,
			body: resp.String(),
		}
	}
	return resp.Bytes(), nil
}

type httpStatusError struct {
	url  string
	code int
	body string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("status code %d for url %s: %s", e.code, e.url, e.body)
}
