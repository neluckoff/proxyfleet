package healthcheck

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultControlHealthURL = "http://127.0.0.1:8080/health"

func Run(ctx context.Context) error {
	if strings.TrimSpace(os.Getenv("CONTROL_URL")) != "" {
		return checkAgent(ctx)
	}
	return checkControl(ctx)
}

func checkControl(ctx context.Context) error {
	url := strings.TrimSpace(os.Getenv("HEALTHCHECK_URL"))
	if url == "" {
		url = defaultControlHealthURL
	}
	return request(ctx, &http.Client{Timeout: 4 * time.Second}, url)
}

func checkAgent(ctx context.Context) error {
	socketPath := strings.TrimSpace(os.Getenv("AGENT_HEALTH_SOCKET"))
	if socketPath == "" {
		socketPath = "/tmp/proxyfleet-agent-health.sock"
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()
	return request(ctx, &http.Client{Transport: transport, Timeout: 4 * time.Second}, "http://proxyfleet/health")
}

func request(ctx context.Context, client *http.Client, url string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}
	return nil
}
