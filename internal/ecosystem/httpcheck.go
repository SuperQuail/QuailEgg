package ecosystem

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

var retrySchedule = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}

// SetRetryWait replaces the waits between registry query attempts and returns a
// function that restores the default schedule. It exists for tests, which must
// not spend the real 22 seconds of backoff; production code never calls it.
func SetRetryWait(schedule []time.Duration) func() {
	previous := retrySchedule
	retrySchedule = append([]time.Duration(nil), schedule...)
	return func() { retrySchedule = previous }
}

// CheckURL asks a registry whether an endpoint exists, retrying transient
// failures. It is the shared precheck used by every publisher.
func CheckURL(ctx context.Context, client *http.Client, endpoint string) (Status, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	var lastErr error
	for attempt := 0; attempt <= len(retrySchedule); attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return Unknown, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusOK:
				return Exists, nil
			case http.StatusNotFound:
				return Absent, nil
			default:
				lastErr = fmt.Errorf("registry returned %s", resp.Status)
			}
		}
		if attempt < len(retrySchedule) {
			select {
			case <-ctx.Done():
				return Unknown, ctx.Err()
			case <-time.After(retrySchedule[attempt]):
			}
		}
	}
	return Unknown, fmt.Errorf("registry check failed: %w", lastErr)
}
